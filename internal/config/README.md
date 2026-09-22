# internal/config

Application configuration loader.

## Purpose

Provides a single `Config` struct populated from environment variables. Centralises all configuration access so that no other module reads `os.Getenv` directly.

## Structure

```
internal/config/
  config.go       # Config struct definition and Load() function
  config_test.go  # Tests for environment parsing, including empty-vs-unset
  README.md       # This file
```

## Key Components

- **`Config`** — Struct with fields: `DatabaseURL`, `RedisURL`, `AppEnv`, `AppPort`, `AdminUser`, `AdminPassword`, `OTELEndpoint`, `OTELServiceName`, `UnkeyRootKey`, `UnkeyAPIID`, `MetricsPort`.
- **`Load() (*Config, error)`** — Reads environment variables. `DATABASE_URL` is required (returns error if missing). All other fields have sensible defaults (`localhost:6379`, `development`, `8080`).
- **`getEnv(key, fallback)`** — Internal helper that returns the env var value or falls back to a default. An empty value is treated as unset.
- **`getEnvAllowEmpty(key, fallback)`** — Like `getEnv`, but an explicitly empty value is returned as-is rather than replaced by the fallback. Used for settings where `""` is meaningful — currently `METRICS_PORT`, where empty disables the metrics listener.

## Dependencies

Standard library only (`os`, `fmt`).

## Usage

```go
cfg, err := config.Load()
if err != nil {
    // DATABASE_URL is not set
}
fmt.Println(cfg.AppPort) // "8080"
```

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `REDIS_URL` | No | `localhost:6379` | Redis address (host:port or URL) |
| `APP_ENV` | No | `development` | Runtime environment |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `METRICS_PORT` | No | `9091` | Internal Prometheus port for the API and worker; empty disables the metrics listener |
| `ADMIN_USER` | No | `admin` | Admin panel HTTP Basic Auth username |
| `ADMIN_PASSWORD` | No | `changeme` (dev only) | Admin panel HTTP Basic Auth password; must be set explicitly in non-development environments |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OpenTelemetry OTLP exporter endpoint (no-op when empty) |
| `OTEL_SERVICE_NAME` | No | `llm-pricing-api` | OpenTelemetry service name |
| `UNKEY_ROOT_KEY` | No | — | Unkey root key for API key verification (required for auth middleware) |
| `UNKEY_API_ID` | No | — | Unkey API ID that keys belong to (required for auth middleware) |
| `MAGIC_LINK_SIGNING_SECRET` | Yes (non-dev) | random in dev | Signs session cookies and keys the HMAC used to hash IPs, emails, user codes, and device codes before they become Redis keys |
| `MAGIC_LINK_TTL_MINUTES` | No | `15` | Magic-link token lifetime |
| `MAGIC_LINK_BASE_URL` | No | `https://llmrates.live` | Frontend base URL used to build magic links and the agent approval URL |
| `MAGIC_LINK_PATH` | No | `/signup/verify` | Path the verify endpoint lives at |
| `SIGNUP_SESSION_COOKIE_NAME` | No | `llmrates_signup` | Name of the signed session cookie |
| `SIGNUP_SESSION_TTL_HOURS` | No | `24` | Session cookie lifetime |
| `SIGNUP_ENABLED` | No | `true` | Kill switch for key issuance; `false` makes `request-link` and the agent device-grant endpoints return 503 |
| `AGENT_GRANT_TTL_MINUTES` | No | `10` | How long an agent device-authorization grant stays redeemable |
