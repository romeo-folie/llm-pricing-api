package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	RedisURL    string
	AppEnv      string
	AppPort     string
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; all other fields have sensible local defaults.
func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required but not set")
	}
	return &Config{
		DatabaseURL: dbURL,
		RedisURL:    getEnv("REDIS_URL", "localhost:6379"),
		AppEnv:      getEnv("APP_ENV", "development"),
		AppPort:     getEnv("APP_PORT", "8080"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
