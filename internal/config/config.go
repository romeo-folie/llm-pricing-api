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
	// SignupEnabled controls whether the free-key signup endpoints are mounted.
	// Accepts any value recognised by strconv.ParseBool (e.g. "true", "TRUE", "1").
	// Defaults to false (off by default until DNS/Resend configured).
	SignupEnabled bool
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
		SignupEnabled:    parseBoolEnv("SIGNUP_ENABLED"),
	}, nil
}

// parseBoolEnv returns the boolean value of the named env var using
// strconv.ParseBool (accepts "1", "t", "TRUE", "true", etc.).
// Returns false when the variable is empty or unparseable.
func parseBoolEnv(key string) bool {
	v, _ := strconv.ParseBool(os.Getenv(key))
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
