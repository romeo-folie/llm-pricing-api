package signup

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// HandlerConfig holds runtime configuration for the signup handlers.
type HandlerConfig struct {
	// Resend
	ResendAPIKey string
	EmailFrom    string

	// Token generation
	SigningSecret  string
	TokenTTL       time.Duration // default 15m
	MagicLinkBase  string        // e.g. https://llmrates.live
	MagicLinkPath  string        // e.g. /auth/signup/verify — the backend verify endpoint, NOT a frontend route

	// Unkey
	UnkeyRootKey string
	UnkeyAPIID   string

	// Session
	SessionTTL        time.Duration // default 24h
	SessionCookieName string        // default llmrates_signup
	SessionSecure     bool          // default true in production
	SessionSameSite   string        // default Lax; configurable via SIGNUP_SESSION_SAMESITE

	// Abuse controls
	ResendCooldown     time.Duration // default 60s
	MaxRequestsPerHour int           // per-IP; default 5
	BlockDisposable    bool
	RegenerateCooldown time.Duration // default 0 (no cooldown)
}

// LoadHandlerConfig reads all signup env vars with sensible defaults.
// Returns an error if any required env var is missing.
func LoadHandlerConfig() (*HandlerConfig, error) {
	secret := os.Getenv("MAGIC_LINK_SIGNING_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("MAGIC_LINK_SIGNING_SECRET is required")
	}
	// Enforce a 32-byte minimum — a shorter HMAC key materially weakens token
	// and session integrity. Fail fast so misconfigured deployments are caught
	// at startup rather than at first request.
	if len(secret) < 32 {
		return nil, fmt.Errorf("MAGIC_LINK_SIGNING_SECRET must be at least 32 bytes (got %d)", len(secret))
	}

	tokenTTL, err := parseDurationMinutes("MAGIC_LINK_TTL_MINUTES", 15)
	if err != nil {
		return nil, err
	}
	sessionTTL, err := parseDurationHours("SIGNUP_SESSION_TTL_HOURS", 24)
	if err != nil {
		return nil, err
	}
	resendCooldown, err := parseDurationSeconds("SIGNUP_RESEND_COOLDOWN_SECONDS", 60)
	if err != nil {
		return nil, err
	}
	maxReqPerHour, err := getEnvInt("SIGNUP_MAX_REQUESTS_PER_IP_PER_HOUR", 5)
	if err != nil {
		return nil, err
	}
	regenCooldown, err := parseDurationSeconds("SIGNUP_REGENERATE_COOLDOWN_SECONDS", 0)
	if err != nil {
		return nil, err
	}
	sessionSecure, err := getEnvBool("SIGNUP_SESSION_SECURE", true)
	if err != nil {
		return nil, err
	}
	blockDisposable, err := getEnvBool("SIGNUP_DISPOSABLE_EMAIL_BLOCK", true)
	if err != nil {
		return nil, err
	}

	cfg := &HandlerConfig{
		ResendAPIKey:       os.Getenv("RESEND_API_KEY"),
		EmailFrom:          getEnvOr("EMAIL_FROM", "noreply@llmrates.live"),
		SigningSecret:      secret,
		TokenTTL:           tokenTTL,
		MagicLinkBase:      getEnvOr("MAGIC_LINK_BASE_URL", "https://llmrates.live"),
		MagicLinkPath:      getEnvOr("MAGIC_LINK_PATH", "/auth/signup/verify"),
		UnkeyRootKey:       os.Getenv("UNKEY_ROOT_KEY"),
		UnkeyAPIID:         os.Getenv("UNKEY_API_ID"),
		SessionTTL:         sessionTTL,
		SessionCookieName:  getEnvOr("SIGNUP_SESSION_COOKIE_NAME", "llmrates_signup"),
		SessionSecure:      sessionSecure,
		SessionSameSite:    getEnvOr("SIGNUP_SESSION_SAMESITE", "Lax"),
		ResendCooldown:     resendCooldown,
		MaxRequestsPerHour: maxReqPerHour,
		BlockDisposable:    blockDisposable,
		RegenerateCooldown: regenCooldown,
	}

	// TokenTTL must be positive — a zero or negative value would cause the email
	// to say "expires in 0 minutes" while GenerateToken silently falls back to 15m.
	if cfg.TokenTTL <= 0 {
		return nil, fmt.Errorf("MAGIC_LINK_TTL_MINUTES must be > 0 (got %v)", cfg.TokenTTL)
	}

	// Validate SameSite — only Lax, Strict, None are valid values.
	// SameSite=None requires Secure=true (modern browser requirement).
	switch cfg.SessionSameSite {
	case "Lax", "Strict", "None":
		// valid
	default:
		return nil, fmt.Errorf("SIGNUP_SESSION_SAMESITE must be one of Lax, Strict, or None (got %q)", cfg.SessionSameSite)
	}
	if cfg.SessionSameSite == "None" && !cfg.SessionSecure {
		return nil, fmt.Errorf("SIGNUP_SESSION_SAMESITE=None requires SIGNUP_SESSION_SECURE=true (required by modern browsers)")
	}

	// Validate required env vars so misconfigured deployments fail at startup
	// rather than at first request (Resend/Unkey calls would silently fail).
	if cfg.ResendAPIKey == "" {
		return nil, fmt.Errorf("RESEND_API_KEY is required when signup is enabled")
	}
	if cfg.UnkeyRootKey == "" {
		return nil, fmt.Errorf("UNKEY_ROOT_KEY is required when signup is enabled")
	}
	if cfg.UnkeyAPIID == "" {
		return nil, fmt.Errorf("UNKEY_API_ID is required when signup is enabled")
	}

	return cfg, nil
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("%s: invalid integer value %q", key, v)
		}
		return n, nil
	}
	return def, nil
}

func getEnvBool(key string, def bool) (bool, error) {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("%s: invalid boolean value %q", key, v)
		}
		return b, nil
	}
	return def, nil
}

func parseDurationMinutes(key string, defMinutes int) (time.Duration, error) {
	n, err := getEnvInt(key, defMinutes)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Minute, nil
}

func parseDurationHours(key string, defHours int) (time.Duration, error) {
	n, err := getEnvInt(key, defHours)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Hour, nil
}

func parseDurationSeconds(key string, defSeconds int) (time.Duration, error) {
	n, err := getEnvInt(key, defSeconds)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Second, nil
}
