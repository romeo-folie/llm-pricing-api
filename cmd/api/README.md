# cmd/api

HTTP API server entrypoint for the LLM Pricing Platform.

## Purpose

Starts the Fiber HTTP server, connects to PostgreSQL and Redis, registers routes, and handles graceful shutdown. This is the main binary users run to serve the REST API.

## Structure

```
cmd/api/
  main.go      # Application entrypoint — config, DB/Redis connect, routes, shutdown
  README.md    # This file
```

## Key Components

- **`main()`** — Loads `.env` via godotenv, reads config, connects to PostgreSQL (with 5-attempt retry) and Redis, registers middleware (logger, recover), mounts the `/health` endpoint, and listens on the configured port. Blocks on `SIGINT`/`SIGTERM` for graceful shutdown.
- **`/health`** — Deep health check that pings both Postgres and Redis. Returns `{"status":"ok"}` (200) when healthy, or `{"status":"degraded"}` (503) when either dependency is unreachable.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/config` | Loads environment variables into a `Config` struct |
| `internal/database` | Creates and configures the pgxpool connection pool |
| `internal/cache` | Creates and configures the Redis client |
| `github.com/gofiber/fiber/v2` | HTTP framework |
| `github.com/joho/godotenv` | `.env` file loading |

## Usage

```bash
# Run directly
go run ./cmd/api

# Build and run
go build -o bin/api ./cmd/api
./bin/api

# Via Makefile
make run
```

Requires `DATABASE_URL` environment variable to be set. See `.env.example` for all configuration options.
