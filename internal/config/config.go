package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL     string
	RedisURL        string
	AppEnv          string
	AppPort         string
	AdminUser       string
	AdminPassword   string
	OTELEndpoint    string
	OTELServiceName string
	UnkeyRootKey    string
	UnkeyAPIID      string
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
		DatabaseURL:     dbURL,
		RedisURL:        getEnv("REDIS_URL", "localhost:6379"),
		AppEnv:          appEnv,
		AppPort:         getEnv("APP_PORT", "8080"),
		AdminUser:       getEnv("ADMIN_USER", "admin"),
		AdminPassword:   adminPassword,
		OTELEndpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTELServiceName: getEnv("OTEL_SERVICE_NAME", "llm-pricing-api"),
		UnkeyRootKey:    os.Getenv("UNKEY_ROOT_KEY"),
		UnkeyAPIID:      os.Getenv("UNKEY_API_ID"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
