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
- **`/health`** — Deep health check that pings both Postgres and Redis. Returns `{"status":"ok"}` (200) when healthy, or `{"status":"degraded"}` (503) when either dependency is unreachable. Both pings run under one 2-second shared deadline (`internal/health`), so an unreachable dependency yields a fast 503 rather than hanging the probe.
- **Request timeout** — `/v1`, `/auth` and `/admin` requests carry a 15-second deadline (`middleware.RequestTimeout`), so a stalled dependency releases its pooled connection instead of pinning it forever. `/v1/stream/*` is exempt: an SSE connection is expected to outlive any request deadline.
- **Liveness watchdog** — probes PostgreSQL every 30 seconds and, after 3 consecutive failures, shuts the server down and exits non-zero so Railway's `ON_FAILURE` restart policy restarts the container. Railway's own healthcheck is a one-time deploy gate and will not: during the four-day outage in issue #183 it reported `SUCCESS` throughout. It watches the database specifically — pool exhaustion is what wedges the process, whereas a Redis outage is survivable (cache, rate limiting and auth fail open) and must not cause a crash loop. `restartPolicyMaxRetries` is 10 to give the watchdog room to act.

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
