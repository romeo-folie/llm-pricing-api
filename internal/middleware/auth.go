// Package middleware provides Fiber middleware for authentication and rate limiting.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	unkeygo "github.com/unkeyed/unkey-go"
	"github.com/unkeyed/unkey-go/models/components"

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

// tierOrder defines the precedence of API key tiers (ascending).
var tierOrder = map[string]int{
	TierFree:      0,
	TierDeveloper: 1,
	TierPro:       2,
}

// UnkeyVerifier abstracts the Unkey key-verification call so the middleware
// can be tested without hitting the real Unkey API.
type UnkeyVerifier interface {
	VerifyKey(ctx context.Context, key, apiID string) (valid bool, tier string, err error)
}

// unkeyClient wraps the official Unkey Go SDK and implements UnkeyVerifier.
type unkeyClient struct {
	sdk   *unkeygo.Unkey
	apiID string
}

// NewUnkeyClient creates a real UnkeyVerifier backed by the Unkey Go SDK.
func NewUnkeyClient(rootKey, apiID string) UnkeyVerifier {
	sdk := unkeygo.New(unkeygo.WithSecurity(rootKey))
	return &unkeyClient{sdk: sdk, apiID: apiID}
}

// VerifyKey calls the Unkey API. The apiID parameter is provided for
// interface compatibility but is ignored — the stored u.apiID is used instead.
func (u *unkeyClient) VerifyKey(ctx context.Context, key, _ string) (bool, string, error) {
	apiID := unkeygo.String(u.apiID)
	res, err := u.sdk.Keys.VerifyKey(ctx, components.V1KeysVerifyKeyRequest{
		Key:   key,
		APIID: apiID,
	})
	if err != nil {
		return false, "", fmt.Errorf("unkey verify: %w", err)
	}
	body := res.V1KeysVerifyKeyResponse
	if body == nil || !body.Valid {
		return false, "", nil
	}

	tier := TierFree
	if body.Meta != nil {
		if v, ok := body.Meta["tier"]; ok {
			if s, ok := v.(string); ok && s != "" {
				tier = strings.ToLower(s)
			}
		}
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

// RequireTier returns a middleware that enforces a minimum tier.
// Tiers are ordered: free < developer < pro.
// If the request's tier is below minTier the handler returns 403 with a
// tier_required field in the RFC 7807 extensions.
func RequireTier(minTier string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		actual, _ := c.Locals(LocalKeyTier).(string)
		if !tierAtLeast(actual, minTier) {
			pd := api.NewForbidden(fmt.Sprintf("this endpoint requires the %s tier or above", minTier))
			pd.Extensions = map[string]any{"tier_required": minTier}
			return pd
		}
		return c.Next()
	}
}

// tierAtLeast returns true when actual >= required in the tier order.
func tierAtLeast(actual, required string) bool {
	return tierOrder[actual] >= tierOrder[required]
}
