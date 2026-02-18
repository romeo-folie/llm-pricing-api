# internal/config

Application configuration loader.

## Purpose

Provides a single `Config` struct populated from environment variables. Centralises all configuration access so that no other module reads `os.Getenv` directly.

## Structure

```
internal/config/
  config.go    # Config struct definition and Load() function
  README.md    # This file
```

## Key Components

- **`Config`** — Struct with fields: `DatabaseURL`, `RedisURL`, `AppEnv`, `AppPort`.
- **`Load() (*Config, error)`** — Reads environment variables. `DATABASE_URL` is required (returns error if missing). All other fields have sensible defaults (`localhost:6379`, `development`, `8080`).
- **`getEnv(key, fallback)`** — Internal helper that returns the env var value or falls back to a default.

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
