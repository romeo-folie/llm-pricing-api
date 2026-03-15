package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL      string
	RedisURL         string
	AppEnv           string
	AppPort          string
	AdminUser        string
	AdminPassword    string
	OTELEndpoint     string
	OTELServiceName  string
	UnkeyRootKey     string
	UnkeyAPIID       string
	// WebhookSecretKey is a 32-byte hex-encoded AES-256-GCM key used to encrypt
	// webhook secrets at rest. If empty, a random ephemeral key is generated at
	// startup — webhook secrets will not survive process restarts in that mode.
	WebhookSecretKey string
	// LogLevel controls the minimum log level. Accepts zerolog level names:
	// trace, debug, info, warn, error, fatal, panic. Defaults to "debug".
	LogLevel string
	// MetricsPort is the port for the internal Prometheus /metrics HTTP server.
	// Defaults to "9091". Set to empty to disable.
	MetricsPort string

	// ── Magic-link signup (Epic #69) ─────────────────────────────────────────

	// ResendAPIKey is the Resend API key used to send magic-link emails.
	ResendAPIKey string
	// EmailFrom is the sender address for outbound magic-link emails.
	// Example: "LLMRates <noreply@llmrates.live>"
	EmailFrom string
	// MagicLinkSigningSecret is a random secret used to HMAC-sign raw tokens
	// before hashing for storage. Prevents offline brute-force of token_hash.
	MagicLinkSigningSecret string
	// MagicLinkTTLMinutes is the lifetime of a magic-link token. Defaults to 15.
	MagicLinkTTLMinutes int
	// MagicLinkBaseURL is the public base URL of the site (no trailing slash).
	// Example: "https://llmrates.live"
	MagicLinkBaseURL string
	// MagicLinkPath is the path the verify endpoint lives at.
	// Example: "/signup/verify"
	MagicLinkPath string
	// SignupSessionCookieName is the name of the session cookie set after
	// successful verification. Defaults to "llmrates_signup".
	SignupSessionCookieName string
	// SignupSessionTTLHours is the lifetime of the session cookie. Defaults to 24.
	SignupSessionTTLHours int
	// SignupSessionSecure controls the Secure flag on the session cookie.
	// Defaults to true; set to false only in local dev (HTTP).
	SignupSessionSecure bool
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; all other fields have sensible local defaults.
// In non-development environments ADMIN_PASSWORD must be explicitly set to a
// non-default value — the default "changeme" is rejected at startup to prevent
// accidentally exposing the admin review queue with a well-known credential.
func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required but not set")
	}

	const defaultAdminPassword = "changeme"
	appEnv := getEnv("APP_ENV", "development")
	adminPassword := getEnv("ADMIN_PASSWORD", defaultAdminPassword)
	if appEnv != "development" && adminPassword == defaultAdminPassword {
		return nil, fmt.Errorf("ADMIN_PASSWORD must be explicitly set in non-development environments")
	}

	return &Config{
		DatabaseURL:      dbURL,
		RedisURL:         getEnv("REDIS_URL", "localhost:6379"),
		AppEnv:           appEnv,
		AppPort:          getEnv("APP_PORT", "8080"),
		AdminUser:        getEnv("ADMIN_USER", "admin"),
		AdminPassword:    adminPassword,
		OTELEndpoint:     os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTELServiceName:  getEnv("OTEL_SERVICE_NAME", "llm-pricing-api"),
		UnkeyRootKey:     os.Getenv("UNKEY_ROOT_KEY"),
		UnkeyAPIID:       os.Getenv("UNKEY_API_ID"),
		WebhookSecretKey: os.Getenv("WEBHOOK_SECRET_KEY"),
		LogLevel:         getEnv("LOG_LEVEL", "debug"),
		MetricsPort:      getEnv("METRICS_PORT", "9091"),

		ResendAPIKey:            os.Getenv("RESEND_API_KEY"),
		EmailFrom:               getEnv("EMAIL_FROM", "LLMRates <noreply@llmrates.live>"),
		MagicLinkSigningSecret:  os.Getenv("MAGIC_LINK_SIGNING_SECRET"),
		MagicLinkTTLMinutes:     getEnvInt("MAGIC_LINK_TTL_MINUTES", 15),
		MagicLinkBaseURL:        getEnv("MAGIC_LINK_BASE_URL", "https://llmrates.live"),
		MagicLinkPath:           getEnv("MAGIC_LINK_PATH", "/signup/verify"),
		SignupSessionCookieName: getEnv("SIGNUP_SESSION_COOKIE_NAME", "llmrates_signup"),
		SignupSessionTTLHours:   getEnvInt("SIGNUP_SESSION_TTL_HOURS", 24),
		SignupSessionSecure:     getEnv("APP_ENV", "development") != "development",
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvIntPositive(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse error: %w", err)
	}
	if i <= 0 {
		return 0, fmt.Errorf("value must be positive, got %d", i)
	}
	return i, nil
}
