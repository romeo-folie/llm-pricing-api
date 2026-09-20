package config

import (
	"os"
	"testing"
)

// requireMinimalEnv supplies the minimum environment Load needs so each test
// can focus on a single variable.
func requireMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("APP_ENV", "development")
}

// TestLoadMetricsPortUnsetDefaultsTo9091 covers a genuinely absent variable.
func TestLoadMetricsPortUnsetDefaultsTo9091(t *testing.T) {
	requireMinimalEnv(t)
	// t.Setenv registers the key for automatic restoration; remove it
	// afterwards so Load observes an unset variable rather than an empty one.
	t.Setenv("METRICS_PORT", "unused")
	if err := os.Unsetenv("METRICS_PORT"); err != nil {
		t.Fatalf("unset METRICS_PORT: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsPort != "9091" {
		t.Errorf("MetricsPort = %q, want %q", cfg.MetricsPort, "9091")
	}
}

// TestLoadMetricsPortExplicit covers an operator choosing a port.
func TestLoadMetricsPortExplicit(t *testing.T) {
	requireMinimalEnv(t)
	t.Setenv("METRICS_PORT", "9199")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsPort != "9199" {
		t.Errorf("MetricsPort = %q, want %q", cfg.MetricsPort, "9199")
	}
}

// TestLoadMetricsPortEmptyDisables is the regression test for the bug this
// change fixes. getEnv treated an empty value as "unset" and returned the
// fallback, so METRICS_PORT="" silently started the metrics listener on 9091
// instead of disabling it — while cmd/api, cmd/worker, .env.example, DEPLOY.md,
// and internal/metrics/README.md all documented empty as "disabled".
func TestLoadMetricsPortEmptyDisables(t *testing.T) {
	requireMinimalEnv(t)
	t.Setenv("METRICS_PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsPort != "" {
		t.Errorf("MetricsPort = %q, want empty (disabled)", cfg.MetricsPort)
	}
}

// TestGetEnvAllowEmpty documents the helper contract the tests above rely on:
// set, empty, and unset are three distinct outcomes.
func TestGetEnvAllowEmpty(t *testing.T) {
	t.Setenv("TEST_ALLOW_EMPTY", "value")
	if got := getEnvAllowEmpty("TEST_ALLOW_EMPTY", "fallback"); got != "value" {
		t.Errorf("set: got %q, want %q", got, "value")
	}

	t.Setenv("TEST_ALLOW_EMPTY", "")
	if got := getEnvAllowEmpty("TEST_ALLOW_EMPTY", "fallback"); got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}

	if err := os.Unsetenv("TEST_ALLOW_EMPTY"); err != nil {
		t.Fatalf("unset TEST_ALLOW_EMPTY: %v", err)
	}
	if got := getEnvAllowEmpty("TEST_ALLOW_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("unset: got %q, want %q", got, "fallback")
	}
}
