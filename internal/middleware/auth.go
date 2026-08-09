// Package middleware provides Fiber middleware for authentication and rate limiting.
package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/api"
)

const (
	// TierFree is the free tier name stored in Unkey key metadata.
	TierFree = "free"
	// TierDeveloper is the developer tier name.
	TierDeveloper = "developer"
	// TierPro is the pro tier name.
	TierPro = "pro"

	// unkeyTTL is how long Unkey validation results are cached in Redis.
	unkeyTTL = 30 * time.Second

	// LocalKeyTier is the fiber.Ctx locals key for the authenticated tier.
	LocalKeyTier = "tier"
	// LocalKeyHash is the fiber.Ctx locals key for the SHA-256 hash of the raw key.
	LocalKeyHash = "key_hash"
)

// UnkeyVerifier abstracts the Unkey key-verification call so the middleware
// can be tested without hitting the real Unkey API.
type UnkeyVerifier interface {
	VerifyKey(ctx context.Context, key, apiID string) (valid bool, tier string, err error)
}

// unkeyClient uses Unkey v2 HTTP API directly and implements UnkeyVerifier.
// We avoid the old v1 SDK endpoint (api.unkey.dev), which no longer resolves.
type unkeyClient struct {
	rootKey string
	apiID   string // kept for interface compatibility / future policy checks
	http    *http.Client
}

// NewUnkeyClient creates a real UnkeyVerifier backed by Unkey v2 HTTP API.
func NewUnkeyClient(rootKey, apiID string) UnkeyVerifier {
	return &unkeyClient{
		rootKey: rootKey,
		apiID:   apiID,
		http:    &http.Client{Timeout: 8 * time.Second},
	}
}

// VerifyKey calls POST https://api.unkey.com/v2/keys.verifyKey with payload {key}.
// v2 infers API from the key itself; apiID is no longer required in request body.
func (u *unkeyClient) VerifyKey(ctx context.Context, key, _ string) (bool, string, error) {
	payload, _ := json.Marshal(map[string]string{"key": key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.unkey.com/v2/keys.verifyKey", bytes.NewReader(payload))
	if err != nil {
		return false, "", fmt.Errorf("unkey request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.rootKey)

	resp, err := u.http.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("unkey verify: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var body struct {
		Data struct {
			Valid bool `json:"valid"`
			Meta  struct {
				Tier string `json:"tier"`
			} `json:"meta"`
		} `json:"data"`
		Error *struct {
			Detail string `json:"detail"`
			Status int    `json:"status"`
			Title  string `json:"title"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return false, "", fmt.Errorf("unkey decode: %w", err)
	}
	if body.Error != nil {
		return false, "", fmt.Errorf("unkey error %d: %s", body.Error.Status, body.Error.Detail)
	}
	if !body.Data.Valid {
		return false, "", nil
	}

	tier := TierFree
	if body.Data.Meta.Tier != "" {
		tier = strings.ToLower(body.Data.Meta.Tier)
	}
	return true, tier, nil
}

// cachedVerifyResult is what we persist in Redis for a verified key.
type cachedVerifyResult struct {
	Valid bool   `json:"valid"`
	Tier  string `json:"tier"`
}

// keyHash returns the hex-encoded SHA-256 digest of rawKey.
// The raw key value is NEVER stored in Redis, logs, or error messages.
func keyHash(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return fmt.Sprintf("%x", sum)
}

// authConfig holds runtime dependencies for the Auth middleware.
type authConfig struct {
	verifier UnkeyVerifier
	redis    *redis.Client
	apiID    string
}

// Auth returns a Fiber middleware that:
//  1. Extracts the Bearer token from Authorization header.
//  2. Checks Redis cache (key: unkey:{sha256(raw_key)}) for a prior result.
//  3. On cache miss, calls Unkey and caches the result for 30 s.
//  4. Stores the tier in c.Locals("tier") and the key hash in c.Locals("key_hash").
//  5. Returns RFC 7807 401 for missing/malformed headers and invalid keys.
func Auth(verifier UnkeyVerifier, redisClient *redis.Client, apiID string) fiber.Handler {
	cfg := &authConfig{verifier: verifier, redis: redisClient, apiID: apiID}
	return cfg.handle
}

func (cfg *authConfig) handle(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return api.NewUnauthorized("Authorization header is required")
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return api.NewUnauthorized("Authorization header must use Bearer scheme")
	}

	rawKey := strings.TrimPrefix(authHeader, prefix)
	if rawKey == "" {
		return api.NewUnauthorized("Bearer token must not be empty")
	}

	hash := keyHash(rawKey)
	cacheKey := "unkey:" + hash

	// --- cache lookup ---
	var result cachedVerifyResult
	if data, err := cfg.redis.Get(c.Context(), cacheKey).Bytes(); err == nil {
		if jsonErr := json.Unmarshal(data, &result); jsonErr == nil {
			if !result.Valid {
				return api.NewUnauthorized("Invalid API key")
			}
			c.Locals(LocalKeyTier, result.Tier)
			c.Locals(LocalKeyHash, hash)
			return c.Next()
		}
	}

	// --- cache miss: call Unkey ---
	valid, tier, err := cfg.verifier.VerifyKey(c.Context(), rawKey, cfg.apiID)
	if err != nil {
		// Do not expose internal error details to the caller.
		return api.NewUnauthorized("API key verification failed")
	}

	// Persist result (valid or not) in Redis so repeated bad keys are fast.
	result = cachedVerifyResult{Valid: valid, Tier: tier}
	if data, merr := json.Marshal(result); merr == nil {
		// Best-effort; ignore errors so a Redis hiccup doesn't break auth.
		_ = cfg.redis.Set(c.Context(), cacheKey, data, unkeyTTL).Err()
	}

	if !valid {
		return api.NewUnauthorized("Invalid API key")
	}

	c.Locals(LocalKeyTier, tier)
	c.Locals(LocalKeyHash, hash)
	return c.Next()
}
