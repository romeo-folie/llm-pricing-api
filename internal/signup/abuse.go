package signup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ipRateLimitScript atomically increments a counter and ensures it has a TTL.
// On the first increment (count == 1) it sets the expiry. On subsequent
// increments it re-applies the expiry if the key somehow lost its TTL (TTL == -1),
// preventing a transient Redis failure from creating a permanent rate-limit.
var ipRateLimitScript = redis.NewScript(`
  local count = redis.call("INCR", KEYS[1])
  if count == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
  elseif redis.call("TTL", KEYS[1]) == -1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
  end
  return count
`)

// AbuseConfig holds the abuse controls applied to the signup flow.
// Use DefaultAbuseConfig for the production values.
type AbuseConfig struct {
	// SigningSecret keys the HMAC used to hash IPs and emails before they are
	// used as Redis keys. Low-entropy values like IPs are enumerable, so an
	// unkeyed digest would be reversible offline.
	SigningSecret string
	// MaxRequestsPerHour caps request-link calls per IP per hour. 0 disables.
	MaxRequestsPerHour int
	// ResendCooldown is the minimum interval between verification emails to the
	// same address. This is what stops request-link being used to mail-bomb a
	// third party. 0 disables.
	ResendCooldown time.Duration
	// RegenerateCooldown is the minimum interval between key regenerations for
	// one identity. 0 disables.
	RegenerateCooldown time.Duration
	// BlockDisposable rejects known disposable email domains.
	BlockDisposable bool
}

// DefaultAbuseConfig returns the production abuse controls, keyed with secret
// (pass the magic-link signing secret).
func DefaultAbuseConfig(secret string) AbuseConfig {
	return AbuseConfig{
		SigningSecret:      secret,
		MaxRequestsPerHour: 5,
		ResendCooldown:     60 * time.Second,
		RegenerateCooldown: 60 * time.Second,
		BlockDisposable:    true,
	}
}

// AbuseGuard enforces rate-limiting and cooldown controls for the signup flow.
// All limits are tracked in Redis, and every control fails open on a Redis
// error so a cache outage cannot block legitimate signups.
type AbuseGuard struct {
	rdb *redis.Client
	cfg AbuseConfig
	log zerolog.Logger
}

// NewAbuseGuard creates an AbuseGuard backed by the given Redis client.
// A nil client disables every Redis-backed control; the disposable-domain block
// still applies.
func NewAbuseGuard(rdb *redis.Client, cfg AbuseConfig, log zerolog.Logger) *AbuseGuard {
	return &AbuseGuard{rdb: rdb, cfg: cfg, log: log}
}

// CheckRequestLink evaluates all abuse controls for the request-link endpoint.
// Returns a non-nil error (with a user-safe message) when a limit is exceeded.
// Controls applied (in order):
//  1. IP rate limit: max N requests per hour per IP.
//  2. Email resend cooldown: min interval between consecutive emails.
//  3. Optional disposable-domain block.
func (g *AbuseGuard) CheckRequestLink(ctx context.Context, ip, email string) error {
	// 1. Disposable domain block (no Redis needed).
	if g.cfg.BlockDisposable && isDisposableDomain(normalizeEmail(email)) {
		return ErrDisposableDomain
	}

	// Remaining controls require Redis; skip them when unavailable.
	if g.rdb == nil {
		return nil
	}

	// 2. IP hourly rate limit.
	if g.cfg.MaxRequestsPerHour > 0 {
		key := fmt.Sprintf("signup:rl:ip:%s", hashValue(ip, g.cfg.SigningSecret))
		// Atomic INCR + EXPIRE via Lua to avoid a race where EXPIRE fails
		// after INCR, leaving a key with no TTL (permanent rate-limit).
		// If the key already exists but lost its TTL (TTL == -1), re-apply it.
		count, err := ipRateLimitScript.Run(ctx, g.rdb, []string{key}, int(time.Hour.Seconds())).Int64()
		if err != nil {
			// Redis failure → fail open (don't block signups due to cache outage).
			g.log.Error().Err(err).Msg("signup: ip-rate-limit Redis error — failing open")
		} else if int(count) > g.cfg.MaxRequestsPerHour {
			return ErrRateLimited
		}
	}

	// 3. Per-email resend cooldown.
	if g.cfg.ResendCooldown > 0 {
		key := fmt.Sprintf("signup:cooldown:email:%s", hashValue(normalizeEmail(email), g.cfg.SigningSecret))
		set, err := g.rdb.SetNX(ctx, key, "1", g.cfg.ResendCooldown).Result() //nolint:staticcheck
		if err != nil {
			// Redis failure → fail open (don't block signups due to cache outage).
			g.log.Error().Err(err).Msg("signup: resend-cooldown Redis error — failing open")
		} else if !set {
			return ErrResendCooldown
		}
	}

	return nil
}

// CheckRegenerateKey evaluates the optional key-regeneration cooldown.
func (g *AbuseGuard) CheckRegenerateKey(ctx context.Context, identityID string) error {
	if g.rdb == nil || g.cfg.RegenerateCooldown <= 0 {
		return nil
	}
	key := fmt.Sprintf("signup:regen:cooldown:%s", identityID)
	set, err := g.rdb.SetNX(ctx, key, "1", g.cfg.RegenerateCooldown).Result() //nolint:staticcheck
	if err != nil {
		return nil // fail open
	}
	if !set {
		return ErrRegenerateCooldown
	}
	return nil
}

// ─── Sentinel errors (safe to surface to clients) ─────────────────────────────

// ErrRateLimited is returned when IP exceeds the hourly request limit.
var ErrRateLimited = fmt.Errorf("too many requests — try again later")

// ErrResendCooldown is returned when an email was recently sent.
var ErrResendCooldown = fmt.Errorf("a verification email was recently sent — wait before requesting another")

// ErrDisposableDomain is returned when the email domain is on the denylist.
var ErrDisposableDomain = fmt.Errorf("disposable email addresses are not accepted")

// ErrRegenerateCooldown is returned when key regeneration is rate-limited.
var ErrRegenerateCooldown = fmt.Errorf("key regeneration is on cooldown — try again later")

// ─── Helpers ──────────────────────────────────────────────────────────────────

// isDisposableDomain checks against a minimal hardcoded denylist of commonly
// abused disposable email services. Expand as needed.
var disposableDomains = map[string]bool{
	"mailinator.com":  true,
	"guerrillamail.com": true,
	"10minutemail.com": true,
	"throwam.com":     true,
	"yopmail.com":     true,
	"maildrop.cc":     true,
	"trashmail.com":   true,
	"sharklasers.com": true,
	"guerrillamailblock.com": true,
	"spam4.me":        true,
	"tempmail.com":    true,
	"dispostable.com": true,
}

func isDisposableDomain(email string) bool {
	parts := strings.SplitN(strings.ToLower(email), "@", 2)
	if len(parts) != 2 {
		return false
	}
	return disposableDomains[parts[1]]
}
