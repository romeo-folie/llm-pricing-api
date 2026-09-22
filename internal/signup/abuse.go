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
	// MaxAgentGrantsPerHour caps device-grant creation per IP per hour. Each
	// grant is a row and an approval prompt, so this bounds both database churn
	// and the user's exposure to unsolicited approval screens. 0 disables.
	MaxAgentGrantsPerHour int
	// MaxUserCodeAttempts caps how many times one user code may be looked up or
	// decided within UserCodeAttemptWindow. This bounds work against a single
	// known code; it does NOT bound enumeration of the code space, because each
	// guess is a different code with a fresh counter. Use MaxCodeProbes for that.
	// 0 disables.
	MaxUserCodeAttempts int
	// UserCodeAttemptWindow is the sliding window for MaxUserCodeAttempts.
	UserCodeAttemptWindow time.Duration
	// MaxCodeProbes caps code lookups/decisions per IP within CodeProbeWindow.
	// This is the control that actually bounds code-space enumeration, and it is
	// deliberately set BELOW MaxUserCodeAttempts: the per-code counter is global,
	// so if one caller could spend the whole per-code budget they could lock the
	// legitimate approver out for the window. Keeping this lower leaves headroom
	// for the real approver. 0 disables.
	MaxCodeProbes int
	// CodeProbeWindow is the sliding window for MaxCodeProbes.
	CodeProbeWindow time.Duration
	// AgentPollInterval is the minimum interval between token-endpoint polls for
	// one device code. Polls that arrive sooner are answered with slow_down.
	// 0 disables the throttle.
	AgentPollInterval time.Duration
	// BlockDisposable rejects known disposable email domains.
	BlockDisposable bool
}

// DefaultAbuseConfig returns the production abuse controls, keyed with secret
// (pass the magic-link signing secret).
func DefaultAbuseConfig(secret string) AbuseConfig {
	return AbuseConfig{
		SigningSecret:         secret,
		MaxRequestsPerHour:    5,
		ResendCooldown:        60 * time.Second,
		RegenerateCooldown:    60 * time.Second,
		MaxAgentGrantsPerHour: 5,
		MaxUserCodeAttempts:   10,
		UserCodeAttemptWindow: 10 * time.Minute,
		// Below MaxUserCodeAttempts on purpose: see the field comment.
		MaxCodeProbes:     8,
		CodeProbeWindow:   10 * time.Minute,
		AgentPollInterval: AgentPollInterval,
		BlockDisposable:   true,
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

// CheckAgentDevice rate-limits device-grant creation per IP.
// Every control fails open on a Redis error, matching CheckRequestLink, so a
// cache outage cannot block agents from onboarding.
func (g *AbuseGuard) CheckAgentDevice(ctx context.Context, ip string) error {
	if g.rdb == nil || g.cfg.MaxAgentGrantsPerHour <= 0 {
		return nil
	}
	key := fmt.Sprintf("signup:rl:agent-device:%s", hashValue(ip, g.cfg.SigningSecret))
	count, err := ipRateLimitScript.Run(ctx, g.rdb, []string{key}, int(time.Hour.Seconds())).Int64()
	if err != nil {
		g.log.Error().Err(err).Msg("signup: agent-device rate-limit Redis error — failing open")
		return nil
	}
	if int(count) > g.cfg.MaxAgentGrantsPerHour {
		return ErrRateLimited
	}
	return nil
}

// CheckUserCodeAttempt bounds lookups and decisions against a single user code.
//
// This is a per-code brake, not an enumeration defence: an attacker enumerating
// the code space tries a different code each time, and each one starts a fresh
// counter. Use CheckCodeProbe for the per-caller bound.
//
// The code is hashed before use as a Redis key: it is low-entropy and would
// otherwise be enumerable offline from a Redis dump.
func (g *AbuseGuard) CheckUserCodeAttempt(ctx context.Context, userCode string) error {
	if g.rdb == nil || g.cfg.MaxUserCodeAttempts <= 0 {
		return nil
	}
	window := g.cfg.UserCodeAttemptWindow
	if window <= 0 {
		window = 10 * time.Minute
	}
	key := fmt.Sprintf("signup:rl:user-code:%s", hashValue(userCode, g.cfg.SigningSecret))
	count, err := ipRateLimitScript.Run(ctx, g.rdb, []string{key}, int(window.Seconds())).Int64()
	if err != nil {
		g.log.Error().Err(err).Msg("signup: user-code rate-limit Redis error — failing open")
		return nil
	}
	if int(count) > g.cfg.MaxUserCodeAttempts {
		return ErrUserCodeAttempts
	}
	return nil
}

// CheckCodeProbe bounds code lookups and decisions per IP.
//
// This is the control that bounds code-space enumeration: regardless of which
// code is presented, one caller only gets MaxCodeProbes tries per window. Because
// that limit is lower than the per-code limit, a single caller cannot spend a
// known code's whole attempt budget and lock the legitimate approver out.
func (g *AbuseGuard) CheckCodeProbe(ctx context.Context, ip string) error {
	if g.rdb == nil || g.cfg.MaxCodeProbes <= 0 {
		return nil
	}
	window := g.cfg.CodeProbeWindow
	if window <= 0 {
		window = 10 * time.Minute
	}
	key := fmt.Sprintf("signup:rl:code-probe:%s", hashValue(ip, g.cfg.SigningSecret))
	count, err := ipRateLimitScript.Run(ctx, g.rdb, []string{key}, int(window.Seconds())).Int64()
	if err != nil {
		g.log.Error().Err(err).Msg("signup: code-probe rate-limit Redis error — failing open")
		return nil
	}
	if int(count) > g.cfg.MaxCodeProbes {
		return ErrRateLimited
	}
	return nil
}

// CheckAgentPoll enforces the minimum interval between token-endpoint polls for
// one device code. Returns ErrPollTooFast when the caller polled sooner than
// AgentPollInterval, which the token endpoint answers with slow_down rather than
// an error. The raw device code is hashed before use as a Redis key: it is the
// credential that redeems the key and must never be stored or logged raw.
func (g *AbuseGuard) CheckAgentPoll(ctx context.Context, rawDeviceCode string) error {
	if g.rdb == nil || g.cfg.AgentPollInterval <= 0 || rawDeviceCode == "" {
		return nil
	}
	key := fmt.Sprintf("signup:rl:agent-poll:%s", hashValue(rawDeviceCode, g.cfg.SigningSecret))
	set, err := g.rdb.SetNX(ctx, key, "1", g.cfg.AgentPollInterval).Result() //nolint:staticcheck
	if err != nil {
		g.log.Error().Err(err).Msg("signup: agent-poll throttle Redis error — failing open")
		return nil
	}
	if !set {
		return ErrPollTooFast
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

// ErrUserCodeAttempts is returned when one user code has been tried too often.
var ErrUserCodeAttempts = fmt.Errorf("too many attempts for this code — try again later")

// ErrPollTooFast is returned when a device code is polled more often than
// AgentPollInterval. It is a throttle signal, not a failure: the token endpoint
// maps it onto the RFC 8628 slow_down response.
var ErrPollTooFast = fmt.Errorf("polling too frequently — slow down")

// ─── Helpers ──────────────────────────────────────────────────────────────────

// isDisposableDomain checks against a minimal hardcoded denylist of commonly
// abused disposable email services. Expand as needed.
var disposableDomains = map[string]bool{
	"mailinator.com":         true,
	"guerrillamail.com":      true,
	"10minutemail.com":       true,
	"throwam.com":            true,
	"yopmail.com":            true,
	"maildrop.cc":            true,
	"trashmail.com":          true,
	"sharklasers.com":        true,
	"guerrillamailblock.com": true,
	"spam4.me":               true,
	"tempmail.com":           true,
	"dispostable.com":        true,
}

func isDisposableDomain(email string) bool {
	parts := strings.SplitN(strings.ToLower(email), "@", 2)
	if len(parts) != 2 {
		return false
	}
	return disposableDomains[parts[1]]
}
