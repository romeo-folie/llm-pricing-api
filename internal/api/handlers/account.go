package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	unkeygo "github.com/unkeyed/unkey-go"
	"github.com/unkeyed/unkey-go/models/components"
	"github.com/unkeyed/unkey-go/models/operations"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/billing"
	"llm-pricing-api/internal/middleware"
)

// compile-time check that pgxAccountStore implements AccountStore.
var _ AccountStore = (*pgxAccountStore)(nil)

// AccountSubscription holds the billing_subscriptions columns needed by the
// account dashboard. It is separate from BillingSubscription (which is keyed
// by LS subscription ID) because this lookup is keyed by unkey_key_id.
type AccountSubscription struct {
	LSSubscriptionID string
	UnkeyKeyID       string
	Status           string
	CancelledAt      *time.Time
}

// AccountStore abstracts the DB operations required by AccountHandler.
type AccountStore interface {
	// GetAccountSubscription returns the subscription row for the given
	// Unkey key ID. Returns ErrBillingSubscriptionNotFound when no row exists.
	GetAccountSubscription(ctx context.Context, unkeyKeyID string) (AccountSubscription, error)
}

// pgxAccountStore implements AccountStore against PostgreSQL.
type pgxAccountStore struct {
	db *pgxpool.Pool
}

// NewAccountStore returns an AccountStore backed by pgxpool.
func NewAccountStore(db *pgxpool.Pool) AccountStore {
	return &pgxAccountStore{db: db}
}

// GetAccountSubscription queries billing_subscriptions by unkey_key_id.
func (s *pgxAccountStore) GetAccountSubscription(ctx context.Context, unkeyKeyID string) (AccountSubscription, error) {
	const q = `
		SELECT ls_subscription_id, unkey_key_id, status, cancelled_at
		  FROM billing_subscriptions
		 WHERE unkey_key_id = $1
		 LIMIT 1`

	var sub AccountSubscription
	err := s.db.QueryRow(ctx, q, unkeyKeyID).Scan(
		&sub.LSSubscriptionID,
		&sub.UnkeyKeyID,
		&sub.Status,
		&sub.CancelledAt,
	)
	if err == pgx.ErrNoRows {
		return AccountSubscription{}, ErrBillingSubscriptionNotFound
	}
	if err != nil {
		return AccountSubscription{}, fmt.Errorf("account: get subscription: %w", err)
	}
	return sub, nil
}

// verifyRateLimit is the maximum number of key-verification attempts allowed
// per IP address per hour. Exceeding this returns HTTP 429.
const verifyRateLimit = 10

// AccountHandler handles the /api/account/* endpoints.
//
// Two endpoints are provided:
//   - POST /api/account/verify — accepts a raw API key, verifies it via Unkey,
//     and returns plan/key/usage data for the dashboard. Called unauthenticated
//     by the Next.js app server which then sets a signed session cookie.
//   - GET  /api/account/usage  — returns today's usage counters. Requires the
//     X-Account-Token header (HMAC-SHA256 of keyId with SESSION_SECRET) so only
//     authenticated Next.js server routes can call it without consuming the
//     user's rate limit.
type AccountHandler struct {
	sdk           *unkeygo.Unkey
	apiID         string
	store         AccountStore
	ls            billing.SubscriptionManager
	rdb           *redis.Client
	sessionSecret string
	log           zerolog.Logger
}

// AccountHandlerConfig groups the dependencies for AccountHandler.
type AccountHandlerConfig struct {
	SDK           *unkeygo.Unkey
	APIID         string
	Store         AccountStore
	LS            billing.SubscriptionManager
	// RDB is used for IP-based rate limiting on POST /api/account/verify.
	// When nil, rate limiting is disabled (useful in tests).
	RDB           *redis.Client
	SessionSecret string
	Log           zerolog.Logger
}

// NewAccountHandler constructs an AccountHandler.
func NewAccountHandler(cfg AccountHandlerConfig) *AccountHandler {
	return &AccountHandler{
		sdk:           cfg.SDK,
		apiID:         cfg.APIID,
		store:         cfg.Store,
		ls:            cfg.LS,
		rdb:           cfg.RDB,
		sessionSecret: cfg.SessionSecret,
		log:           cfg.Log,
	}
}

// AccountVerifyRequest is the JSON body for POST /api/account/verify.
type AccountVerifyRequest struct {
	Key string `json:"key"`
}

// AccountVerifyResponse is returned by POST /api/account/verify.
type AccountVerifyResponse struct {
	// Tier is the plan: "free", "developer", or "pro".
	Tier string `json:"tier"`
	// MaskedKey is first-12 + "..." + last-4 characters of the raw key.
	MaskedKey string `json:"maskedKey"`
	// KeyID is the stable Unkey key identifier used for usage lookups.
	KeyID string `json:"keyId"`
	// DailyLimit is the configured daily request limit (-1 for unlimited / Pro tier).
	DailyLimit int64 `json:"dailyLimit"`
	// UsageToday is today's consumed request count (success + rate-limited + exceeded).
	UsageToday int64 `json:"usageToday"`
	// EndsAt is set for cancelled subscriptions with a future expiry date.
	EndsAt *time.Time `json:"endsAt,omitempty"`
	// PortalURL is the Lemon Squeezy customer portal link (paid tiers only, 24h TTL).
	PortalURL string `json:"portalUrl,omitempty"`
}

// AccountUsageResponse is returned by GET /api/account/usage.
type AccountUsageResponse struct {
	// UsageToday is today's consumed request count.
	UsageToday int64 `json:"usageToday"`
	// DailyLimit is the configured daily request limit (-1 for unlimited).
	DailyLimit int64 `json:"dailyLimit"`
}

// HandleVerify processes POST /api/account/verify.
// It is intentionally unauthenticated — the caller (Next.js server route)
// handles session cookie issuance.
//
// Rate limiting: up to verifyRateLimit (10) attempts per IP per hour are
// allowed, enforced via Redis (SETNX + INCR). When rdb is nil (e.g. in unit
// tests) rate limiting is skipped.
//
// Note: VerifyKey is the only Unkey mechanism that validates a raw API key and
// returns the stable KeyID in a single call. This consumes one request from the
// user's rate limit counter. The trade-off is intentional: sign-in happens at
// most once per session hour, making the cost negligible relative to the
// per-session allowance (100–10 000 req/day).
func (h *AccountHandler) HandleVerify(c *fiber.Ctx) error {
	// IP-based rate limiting: prevent brute-force key enumeration.
	if h.rdb != nil {
		ipHash := sha256.Sum256([]byte(c.Context().RemoteIP().String()))
		rlKey := fmt.Sprintf("account:verify:%x", ipHash)
		// Seed the counter at 0 on first attempt (NX = only if key does not exist).
		_ = h.rdb.SetArgs(c.Context(), rlKey, 0, redis.SetArgs{
			Mode: "NX",
			TTL:  time.Hour,
		}).Err()
		count, err := h.rdb.Incr(c.Context(), rlKey).Result()
		if err != nil {
			h.log.Error().Err(err).Msg("account: verify rate limit check failed")
			return api.NewInternalError("could not process request")
		}
		// Safety net: if SETNX failed silently but INCR succeeded, the key
		// exists without a TTL. Set EXPIRE only when the count is 1 (first
		// increment), ensuring the window always has a TTL even under transient
		// Redis errors during the SETNX call.
		if count == 1 {
			_ = h.rdb.Expire(c.Context(), rlKey, time.Hour).Err()
		}
		if count > verifyRateLimit {
			return api.NewTooManyRequests("too many key verification attempts — try again in an hour")
		}
	}

	var req AccountVerifyRequest
	if err := c.BodyParser(&req); err != nil {
		return api.NewBadRequest("body must be JSON with a \"key\" field")
	}
	if req.Key == "" {
		return api.NewBadRequest("key is required")
	}

	// Verify the key against Unkey. We call the SDK directly (not through the
	// middleware.UnkeyVerifier interface) so we can access KeyID and Ratelimit.
	apiID := unkeygo.String(h.apiID)
	res, err := h.sdk.Keys.VerifyKey(c.Context(), components.V1KeysVerifyKeyRequest{
		Key:   req.Key,
		APIID: apiID,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("account: verify key: unkey API error")
		return api.NewInternalError("could not verify key")
	}

	body := res.V1KeysVerifyKeyResponse
	if body == nil || !body.Valid {
		return api.NewUnauthorized("the provided key is not valid")
	}

	// Extract keyId (stable identifier for management API calls).
	if body.KeyID == nil || *body.KeyID == "" {
		return api.NewInternalError("key verification returned no key ID")
	}
	keyID := *body.KeyID

	// Determine tier from Meta["tier"].
	tier := middleware.TierFree
	if body.Meta != nil {
		if v, ok := body.Meta["tier"]; ok {
			if s, ok := v.(string); ok && s != "" {
				tier = s
			}
		}
	}

	// Daily limit from real-time ratelimit data.
	var dailyLimit int64 = -1
	var usageToday int64
	if body.Ratelimit != nil {
		dailyLimit = body.Ratelimit.Limit
		used := body.Ratelimit.Limit - body.Ratelimit.Remaining
		if used > 0 {
			usageToday = used
		}
	}

	out := AccountVerifyResponse{
		Tier:       tier,
		MaskedKey:  maskKey(req.Key),
		KeyID:      keyID,
		DailyLimit: dailyLimit,
		UsageToday: usageToday,
	}

	// For paid tiers look up the subscription and fetch the LS portal URL.
	if tier != middleware.TierFree && h.ls != nil {
		sub, err := h.store.GetAccountSubscription(c.Context(), keyID)
		if err != nil && err != ErrBillingSubscriptionNotFound {
			h.log.Warn().Err(err).Str("key_id", keyID).Msg("account: get subscription")
		}
		if err == nil {
			if sub.CancelledAt != nil {
				out.EndsAt = sub.CancelledAt
			}
			if sub.LSSubscriptionID != "" {
				portalURL, pErr := h.ls.GetCustomerPortalURL(c.Context(), sub.LSSubscriptionID)
				if pErr != nil {
					h.log.Warn().Err(pErr).Str("ls_sub_id", sub.LSSubscriptionID).Msg("account: get portal URL")
				} else {
					out.PortalURL = portalURL
				}
			}
		}
	}

	return c.Status(fiber.StatusOK).JSON(out)
}

// HandleUsage processes GET /api/account/usage.
// It requires the X-Account-Token header, which must be HMAC-SHA256(keyId, sessionSecret).
// This prevents unauthenticated callers from hitting the Unkey management API.
// GetVerifications does NOT consume the user's rate limit.
func (h *AccountHandler) HandleUsage(c *fiber.Ctx) error {
	keyID := c.Query("key_id")
	if keyID == "" {
		return api.NewBadRequest("key_id query param is required")
	}

	token := c.Get("X-Account-Token")
	if token == "" {
		return api.NewUnauthorized("X-Account-Token header is required")
	}
	if !validateAccountToken(keyID, h.sessionSecret, token) {
		return api.NewUnauthorized("X-Account-Token is invalid")
	}

	// Fetch today's verification counts. granularity=day scoped to today (UTC midnight → now).
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startMs := midnight.UnixMilli()
	gran := operations.GranularityDay

	verRes, err := h.sdk.Keys.GetVerifications(c.Context(), operations.KeysGetVerificationsRequest{
		KeyID:       &keyID,
		Start:       &startMs,
		Granularity: &gran,
	})
	if err != nil {
		h.log.Error().Err(err).Str("key_id", keyID).Msg("account: get verifications")
		return api.NewInternalError("could not fetch usage data")
	}

	var usageToday int64
	if verRes.Object != nil {
		for _, v := range verRes.Object.Verifications {
			usageToday += v.Success + v.RateLimited + v.UsageExceeded
		}
	}

	// Fetch the configured daily limit from GetKey.
	var dailyLimit int64 = -1
	keyRes, err := h.sdk.Keys.GetKey(c.Context(), operations.GetKeyRequest{KeyID: keyID})
	if err != nil {
		h.log.Warn().Err(err).Str("key_id", keyID).Msg("account: get key (limit fallback)")
	} else if keyRes.Key != nil && keyRes.Key.Ratelimit != nil {
		dailyLimit = keyRes.Key.Ratelimit.Limit
	}

	return c.Status(fiber.StatusOK).JSON(AccountUsageResponse{
		UsageToday: usageToday,
		DailyLimit: dailyLimit,
	})
}

// validateAccountToken returns true if the given token equals
// HMAC-SHA256(keyID, sessionSecret) encoded as lowercase hex.
// Uses hmac.Equal for constant-time comparison.
func validateAccountToken(keyID, sessionSecret, token string) bool {
	mac := hmac.New(sha256.New, []byte(sessionSecret))
	mac.Write([]byte(keyID))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(token), []byte(expected))
}

// maskKey returns a display-safe version of the raw key:
// first 12 characters + "..." + last 4 characters.
// For very short keys (≤16 chars), it returns the first half masked with "***".
func maskKey(key string) string {
	if len(key) <= 16 {
		half := len(key) / 2
		return key[:half] + "***"
	}
	return key[:12] + "..." + key[len(key)-4:]
}
