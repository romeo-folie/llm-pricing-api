# llm-pricing-api

A reconciled, multi-source pricing data API for large language models.

The API aggregates token pricing from OpenRouter, LiteLLM, and provider documentation pages, reconciles discrepancies across sources, stores an immutable change history in TimescaleDB, and serves it through a versioned REST API with tier-based access control.

**The differentiator is price history and change tracking.** Every competing resource gives a snapshot; this gives the full timeline with source attribution, confidence metadata, and a real-time SSE stream of changes.

---

## Table of Contents

- [Architecture](#architecture)
- [Stack](#stack)
- [Project Structure](#project-structure)
- [API Endpoints](#api-endpoints)
- [Local Development](#local-development)
- [Environment Variables](#environment-variables)
- [Database Migrations](#database-migrations)
- [Deployment](#deployment)
- [Testing](#testing)

---

## Architecture

The system has five layers built in dependency order:

```
┌─────────────────────────────────────────────────────────────┐
│  Data Pipeline (asynq workers)                              │
│                                                             │
│  OpenRouter ──┐                                             │
│  LiteLLM ─────┼──► diff engine ──► reconciler ──► DB write │
│  Provider docs┘          ↕                  ↕              │
│                      5% threshold      2-source             │
│                      → review queue    agreement            │
└─────────────────────────────────────────────────────────────┘
         │ confirmed changes
         ▼
┌──────────────────────────────────────────────────────────────┐
│  Storage                                                      │
│  PostgreSQL + TimescaleDB    Redis                            │
│  ├── sources                 ├── Unkey validation cache       │
│  ├── models                  ├── response cache               │
│  ├── prices (current)        └── asynq job queue             │
│  ├── price_history (hypertable, immutable)                    │
│  ├── review_queue                                             │
│  └── webhooks                                                 │
└──────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────────────────────────────────────────────────────┐
│  REST API (Fiber)                                             │
│                                                              │
│  Middleware chain:                                            │
│  Request → Auth (Unkey) → Rate limit → Cache → Handler       │
│                                                              │
│  Tiers: Free · Developer · Pro                                │
└──────────────────────────────────────────────────────────────┘
```

### Reconciliation rules

- Any source disagreement exceeding 5% → flagged to the review queue (4-hour operator SLA)
- Single-source change → held until 2 consecutive matching fetches confirm it
- Flagged records never auto-resolve; they require a confirmed match or manual operator approval
- Every confirmed change → immutable append to `price_history` with source attribution

### Trust metadata

Every API response includes a `trust` object:

```json
{
  "confirmed_at": "2026-02-18T10:00:00Z",
  "source": "openrouter",
  "confidence": "high",
  "age_hours": 2.4,
  "change_velocity": 0.12
}
```

`confidence` is `high` when two or more independent sources agree, `medium` for single-source confirmed values, and `low` for values awaiting confirmation. This lets API consumers decide at call time whether a price is trustworthy enough to act on.

---

## Stack

### Go 1.24

Go's compile-time type safety, minimal runtime overhead, and first-class concurrency primitives make it a natural fit for a data pipeline that runs multiple scrapers, a diff engine, and a reconciler concurrently. The single static binary simplifies Railway deployment significantly compared to interpreted runtimes.

### Fiber v2

Fiber is built directly on `fasthttp` (a zero-allocation HTTP engine) rather than the standard `net/http` library, which matters when the Unkey validation middleware runs on every request. Its `c.Locals()` context propagation is used to pass the validated tier downstream to handlers without any global state.

### PostgreSQL 16 + TimescaleDB 2.15.3

Plain PostgreSQL was the obvious choice for relational data (models, sources, webhooks). TimescaleDB was added specifically for `price_history`, which is an append-only time-series workload:

- The `price_history` table is a TimescaleDB **hypertable** partitioned by `recorded_at` in 7-day chunks. Time-range queries (e.g. "price history for model X over the last 30 days") hit only the relevant chunks rather than scanning the full table.
- All writes are appends. TimescaleDB's chunk architecture keeps insert performance constant regardless of total row count.
- InfluxDB and ClickHouse were considered but rejected: both would have required a second database connection and a separate mental model for the relational parts of the schema.

### Redis 7.2

Redis serves two independent roles:

1. **asynq job queue** — scraper tasks and webhook deliveries are enqueued as asynq jobs backed by Redis. asynq gives at-least-once delivery, visibility into queued/active/failed jobs, and a built-in cron scheduler without any additional infrastructure.
2. **Response and Unkey validation cache** — Unkey API key validation results are cached for 30 seconds so that the external call does not happen on every request. Response caching for read-heavy endpoints (e.g. `/v1/models`) is also stored in Redis.

Kafka and RabbitMQ were considered for the job queue but rejected: the workload (two scrapers, one webhook delivery queue) does not require their complexity, and Redis was already required for caching.

### pgx v5

pgx is a native PostgreSQL driver with a built-in connection pool (`pgxpool`). It was chosen over `database/sql` + a driver because it gives direct access to PostgreSQL-specific types (arrays, hstore, JSONB), named prepared statements, and pool-level health checks — all needed for the TimescaleDB hypertable queries. ORMs like GORM were rejected: the reconciler's conflict-handling logic requires precise control over transactions that ORMs abstract away poorly.

### asynq v0.26

asynq is a distributed task queue built on Redis. It handles the scraper cron jobs (OpenRouter every 6 hours, LiteLLM every 24 hours) and webhook delivery (at-least-once, 3 retries, exponential backoff). Its dashboard-compatible wire format means queue state is inspectable without custom tooling.

### Unkey

Unkey manages API key creation, validation, and tier metadata. Rather than building key hashing, revocation, and rate-limit infrastructure from scratch, Unkey provides all of it as a service. The middleware validates keys against Unkey's API, extracts the tier claim from the response, and caches the result in Redis for 30 seconds so revocations propagate within one TTL window.

### zerolog v1.34

zerolog writes structured JSON logs with zero allocations on the hot path. In production the log level is set to `info` and every log line carries the OpenTelemetry `trace_id` and `span_id` so logs and traces correlate in the observability backend without additional configuration.

### OpenTelemetry SDK v1.40

The OTel SDK instruments the API and worker with distributed traces. When `OTEL_EXPORTER_OTLP_ENDPOINT` is not set the SDK initialises a no-op provider, so the instrumentation calls are always safe to make and the binary does not need to be rebuilt to enable or disable tracing.

### Railway

Railway was chosen for hosting because it supports Nixpacks (builds the Go binary from source without a Dockerfile), provides first-class PostgreSQL and Redis add-ons with automatic `DATABASE_URL` / `REDIS_URL` injection, and has a zero-configuration health-check integration via `railway.json`. The TimescaleDB template covers the one case where Railway's default Postgres image is insufficient.

---

## Project Structure

```
.
├── cmd/
│   ├── api/            # HTTP server entry point (Fiber app, middleware, routes, graceful shutdown)
│   └── worker/         # Background worker entry point (asynq server + cron scheduler)
├── internal/
│   ├── api/            # HTTP response helpers: RFC 7807 errors, TrustMeta, response envelope
│   │   └── handlers/   # One file per endpoint group; Store interface for DB access
│   ├── cache/          # Redis client initialisation
│   ├── config/         # Environment variable loader (Config struct)
│   ├── database/       # pgxpool initialisation with connection tuning
│   ├── diff/           # Pure-function price diff engine; no I/O
│   ├── logger/         # zerolog setup with OTel trace_id/span_id injection
│   ├── middleware/      # Auth (Unkey + Redis cache), rate limiting, response cache, security headers
│   ├── models/         # Domain types: Model, Source, Price, PriceHistory, ReviewQueueItem
│   ├── otel/           # OpenTelemetry SDK init with no-op fallback
│   ├── reconciler/     # Reconciliation engine: 2-source agreement, discrepancy flagging, webhook fan-out
│   ├── review/         # Admin review queue: HTTP handlers + server-rendered HTML UI
│   ├── scraper/
│   │   ├── openrouter/ # OpenRouter /v1/models scraper (runs every 6 hours)
│   │   └── litellm/    # LiteLLM GitHub JSON scraper (runs every 24 hours)
│   ├── webhooks/       # Webhook domain types
│   └── worker/         # asynq task handlers: scraper pipeline, webhook delivery
├── migrations/         # SQL schema (7 migrations, golang-migrate)
├── docker-compose.yml  # Local postgres + redis
├── Makefile            # Dev workflow targets
├── railway.json        # Railway build/deploy/health-check config
├── DEPLOY.md           # Step-by-step Railway deployment runbook
└── .env.example        # All supported environment variables with descriptions
```

Each package under `internal/` has its own `README.md` explaining its purpose, key types, and usage.

---

## API Endpoints

| Endpoint | Tier | Description |
|---|---|---|
| `GET /v1/models` | Free | List models; filter by `?provider=`, `?modality=`, `?min_context=` |
| `GET /v1/models/:id` | Free | Single model with current prices and trust metadata |
| `GET /v1/providers` | Free | List all known providers |
| `GET /v1/compare?models=` | Free | Side-by-side comparison of up to 5 models |
| `GET /v1/changes` | Free | Recent price changes; filter by `?since=`, `?provider=` |
| `GET /v1/models/:id/history` | Developer+ | Full price history; filter by `?from=`, `?to=` |
| `GET /v1/recommend` | Developer+ | Models ranked by task type, context size, and price |
| `GET /v1/context` | Developer+ | ≤2 100-token pricing snapshot for agent system prompts |
| `GET /v1/stream/changes` | Developer+ | SSE stream of price changes; supports `Last-Event-ID` reconnection |
| `POST /v1/webhooks` | Pro | Register a webhook URL for price-change notifications |
| `DELETE /v1/webhooks/:id` | Pro | Remove a webhook |
| `GET /openapi.json` | Public | OpenAPI 3.1 spec |
| `GET /.well-known/ai-plugin.json` | Public | AI plugin manifest |
| `GET /llms.txt` | Public | Plain-text model index for LLM discovery |
| `GET /health` | Public | Returns `{"status":"ok","db":"ok","redis":"ok"}` |

All authenticated endpoints require `Authorization: Bearer <api-key>`. Error responses follow [RFC 7807 Problem Details](https://www.rfc-editor.org/rfc/rfc7807).

### Rate limits

| Tier | Requests per day |
|---|---|
| Free | 100 |
| Developer | 10 000 |
| Pro | Unlimited |

---

## Local Development

### Prerequisites

- Go 1.24+
- Docker and Docker Compose
- `make`

### Start

```bash
# Copy example env (only needed once)
make setup

# Start PostgreSQL + Redis
make up

# Apply database migrations
make migrate-up

# Run the API server
make run

# In a separate terminal, run the background worker
make worker
```

The API will be available at `http://localhost:8080`.

### Common targets

```bash
make test          # Run all tests
make build         # Compile to bin/api
make migrate-down  # Roll back last migration
make logs          # Stream Docker container logs
make down          # Stop Docker containers
```

---

## Environment Variables

Copy `.env.example` to `.env` before running locally. All variables are described in `.env.example`.

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `REDIS_URL` | No | `localhost:6379` | Redis connection string |
| `APP_ENV` | No | `development` | `development` / `staging` / `production` |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `LOG_LEVEL` | No | `info` | `trace` / `debug` / `info` / `warn` / `error` |
| `ADMIN_USER` | No | `admin` | Basic auth username for `/admin` routes |
| `ADMIN_PASSWORD` | Yes (prod) | `changeme` | Basic auth password — change in production |
| `UNKEY_ROOT_KEY` | Yes | — | Unkey root key for API key verification |
| `UNKEY_API_ID` | Yes | — | Unkey API namespace ID |
| `WEBHOOK_SECRET_KEY` | No | ephemeral | 32-byte hex AES-256-GCM key for webhook secret encryption |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OTLP gRPC endpoint; leave empty to disable tracing |
| `OTEL_SERVICE_NAME` | No | `llm-pricing-api` | Service name in traces and logs |

In production on Railway, `DATABASE_URL` and `REDIS_URL` are injected automatically by the platform when the Timescale and Redis services are linked.

---

## Database Migrations

Migrations live in `migrations/` and are managed by [golang-migrate](https://github.com/golang-migrate/migrate).

```bash
# Install the CLI (once)
make install-tools

# Apply all pending migrations
make migrate-up

# Roll back the last migration
make migrate-down
```

| Migration | Creates |
|---|---|
| 000001 | `timescaledb` extension |
| 000002 | `sources` table (7 provider rows seeded) |
| 000003 | `models` table |
| 000004 | `prices` table (current confirmed price per model+source) |
| 000005 | `price_history` TimescaleDB hypertable (7-day chunks, deduplication index) |
| 000006 | `review_queue` table |
| 000007 | `webhooks` table |

---

## Deployment

See [`DEPLOY.md`](DEPLOY.md) for the full Railway deployment runbook, including:

- Provisioning TimescaleDB and Redis
- Running migrations against the Railway database
- Setting all required environment variables
- Generating a public domain
- Running the load test (`hey -n 10000 -c 100`)

The production deployment is at `https://llm-pricing-api-production.up.railway.app`.

### Performance

Load test results (10 000 requests, 100 concurrent, `/v1/models`, warm cache):

| Percentile | Latency |
|---|---|
| p50 | 128 ms |
| p95 | 154 ms |
| p99 | 399 ms |

p95 is within the 200 ms target. The p99 tail is caused by TCP connection establishment on new connections to Railway's proxy layer; see `loadtest-results.txt` for the full breakdown.

---

## Testing

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/reconciler/...

# Run a single test by name
go test -run TestReconcile ./...

# Run with coverage
go test -cover ./...
```

The test suite uses mock implementations of every storage and external interface (`mockStore`, `mockReconcilerStore`, `mockScraper`, `mockUnkeyVerifier`) so no live database or Redis instance is required to run the tests.
