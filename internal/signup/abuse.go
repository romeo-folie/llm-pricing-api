package signup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// AbuseGuard enforces rate-limiting and cooldown controls for the signup flow.
// All limits are tracked in Redis.
type AbuseGuard struct {
	rdb    *redis.Client
	cfg    *HandlerConfig
}

// NewAbuseGuard creates an AbuseGuard backed by the given Redis client.
func NewAbuseGuard(rdb *redis.Client, cfg *HandlerConfig) *AbuseGuard {
	return &AbuseGuard{rdb: rdb, cfg: cfg}
}

// CheckRequestLink evaluates all abuse controls for the request-link endpoint.
// Returns a non-nil error (with a user-safe message) when a limit is exceeded.
// Controls applied (in order):
//  1. IP rate limit: max N requests per hour per IP.
//  2. Email resend cooldown: min interval between consecutive emails.
//  3. Optional disposable-domain block.
func (g *AbuseGuard) CheckRequestLink(ctx context.Context, ip, email string) error {
	if g.rdb == nil {
		return nil
	}
	// 1. IP hourly rate limit.
	if g.cfg.MaxRequestsPerHour > 0 {
		key := fmt.Sprintf("signup:rl:ip:%s", ip)
		count, err := g.rdb.Incr(ctx, key).Result()
		if err != nil {
			// Redis failure → fail open (don't block signups due to cache outage).
		} else {
			// Set TTL on the first increment; if EXPIRE fails, delete the key to
			// avoid permanently rate-limiting this IP due to a missing TTL (fail-open).
			ttlFailed := false
			if count == 1 {
				if expErr := g.rdb.Expire(ctx, key, time.Hour).Err(); expErr != nil {
					_ = g.rdb.Del(ctx, key)
					ttlFailed = true
				}
			}
			// Perform limit check for every increment (not just count > 1).
			// Skip on TTL failure to stay fail-open.
			if !ttlFailed && int(count) > g.cfg.MaxRequestsPerHour {
				return ErrRateLimited
			}
		}
	}

	// 2. Per-email resend cooldown.
	if g.cfg.ResendCooldown > 0 {
		key := fmt.Sprintf("signup:cooldown:email:%s", normalizeEmail(email))
		set, err := g.rdb.SetNX(ctx, key, "1", g.cfg.ResendCooldown).Result()
		if err == nil && !set {
			return ErrResendCooldown
		}
	}

	// 3. Disposable domain block.
	if g.cfg.BlockDisposable && isDisposableDomain(email) {
		return ErrDisposableDomain
	}

	return nil
}

// CheckRegenerateKey evaluates the optional key-regeneration cooldown.
func (g *AbuseGuard) CheckRegenerateKey(ctx context.Context, identityID string) error {
	if g.rdb == nil || g.cfg.RegenerateCooldown <= 0 {
		return nil
	}
	key := fmt.Sprintf("signup:regen:cooldown:%s", identityID)
	set, err := g.rdb.SetNX(ctx, key, "1", g.cfg.RegenerateCooldown).Result()
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

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
