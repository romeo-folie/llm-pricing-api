# llm-pricing-api

A reconciled, multi-source pricing and capability data API for large language models.

The API aggregates token pricing from OpenRouter, LiteLLM, HuggingFace, and provider documentation pages, reconciles discrepancies across sources, stores an immutable change history in TimescaleDB, and serves it through a versioned REST API. It also ingests SWE-bench Verified and LiveCodeBench evidence to support capability-aware model comparisons.

**The differentiator is price history, change tracking, and capability scoring.** Every competing resource gives a pricing snapshot; this gives the full timeline with source attribution, confidence metadata, a real-time SSE stream of changes, and benchmark-grounded capability scores that let you find the right model — not just the cheapest one.

---

## Table of Contents

- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [API Endpoints](#api-endpoints)
- [Local Development](#local-development)
- [Environment Variables](#environment-variables)
- [Database Migrations](#database-migrations)
- [Deployment](#deployment)
- [Testing](#testing)
- [Benchmark Ingestion](#benchmark-ingestion)

---

## Architecture

Two independent pipelines write to a shared store, which is served through one REST API and consumed by a web frontend and an MCP server.

```
┌───────────────────────────────────────────────────────────────────────────┐
│  Pricing Pipeline (asynq cron workers)                                    │
│                                                                           │
│  OpenRouter      every 6h  ─┐                                             │
│  LiteLLM         every 24h  │                                             │
│  HuggingFace     every 24h  ├─► diff engine ──► reconciler ──► DB write   │
│  OpenAI docs     every 24h  │        │              │                     │
│  Anthropic docs  every 24h  │   >5% delta      2-source                   │
│  Gemini docs     every 24h ─┘   → review queue   agreement                │
│                                                                           │
│  Confirmed changes fan out to Redis Pub/Sub (SSE) and webhook jobs        │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
┌───────────────────────────────────────────────────────────────────────────┐
│  Benchmark Pipeline (asynq cron workers)                                  │
│                                                                           │
│  SWE-bench Verified  every 24h ─┐                                         │
│  LiveCodeBench       every 24h  ├─► slugmap.Resolve() ──► benchmark       │
│  Manual CLI          on demand ─┘                          evidence       │
│                                                             │             │
│                                                             ▼             │
│                                                         capability        │
│                                                           scores          │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌───────────────────────────────────────────────────────────────────────────┐
│  Storage                                                                  │
│  PostgreSQL + TimescaleDB               Redis                             │
│  ├── sources                            ├── Unkey validation cache (30s)  │
│  ├── models                             ├── response cache                │
│  ├── prices (current + last_verified)   ├── rate-limit counters           │
│  ├── price_history (hypertable)         ├── SSE Pub/Sub + replay buffer   │
│  ├── review_queue                       └── asynq job queue               │
│  ├── webhooks                                                             │
│  ├── signup identity + sessions                                           │
│  ├── benchmarks                                                           │
│  ├── model_benchmark_scores                                               │
│  └── model_capability_scores                                              │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌───────────────────────────────────────────────────────────────────────────┐
│  REST API (Fiber)                                                         │
│                                                                           │
│  Global:  Security headers → OTel → Prometheus → logger → recover         │
│  /v1:     → Auth (Unkey) → Response cache → Rate limit → Handler          │
│  /auth:   → IP rate limit → magic-link signup handlers                    │
│  /admin:  → HTTP Basic Auth → review queue UI                             │
│                                                                           │
│  Prometheus /metrics is served on a separate internal port (never public) │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
                     ┌───────────────┴───────────────┐
                     ▼                               ▼
          Frontend (Next.js)                MCP server (@llmrates/mcp)
          llmrates.live                     6 tools over the same API
```

### Reconciliation rules

- Any source disagreement exceeding 5% → flagged to the review queue (4-hour operator SLA)
- Single-source change → held until 2 consecutive matching fetches confirm it
- Flagged records never auto-resolve; they require a confirmed match or manual operator approval
- Every confirmed change → immutable append to `price_history` with source attribution

Scrapers never write pricing data directly. All writes go through the reconciler.

### Trust metadata

Every pricing response carries a trust object so callers can decide at call time whether a value is fresh enough to act on:

```json
{
  "confirmed_at": "2026-02-18T10:00:00Z",
  "last_verified_at": "2026-08-09T06:00:00Z",
  "source": "openrouter",
  "confidence": "high",
  "age_hours": 2.4,
  "change_velocity": 0.12
}
```

The two timestamps mean different things, and the distinction matters:

- `confirmed_at` advances only when the price **value actually changes**.
- `last_verified_at` advances every time a scrape **re-observes the price as still current**, whether or not it moved.

`age_hours` is measured from `last_verified_at`, so a stable price that is re-verified on schedule does not read as stale. `change_velocity` is the count of distinct price values seen in the last 30 days divided by 30.

`confidence` is computed as:

| Value | Condition |
|---|---|
| `high` | Two or more distinct sources agree on the latest input/output price pair |
| `medium` | Single source, verified within the last 24 hours |
| `low` | Single source, last verified more than 24 hours ago — or no data at all |

---

## Project Structure

```
.
├── cmd/
│   ├── api/                    # HTTP server entry point (Fiber app, middleware, routes, graceful shutdown)
│   ├── worker/                 # Background worker entry point (asynq server + cron scheduler)
│   └── tools/
│       └── ingest_benchmark/   # Manual benchmark score ingestion CLI
├── internal/
│   ├── api/                    # RFC 7807 errors, TrustMeta, response envelope
│   │   └── handlers/           # One file per endpoint group; Store interface for DB access
│   ├── auth/                   # Magic-link signup HTTP handlers (/auth routes)
│   ├── cache/                  # Redis client initialisation
│   ├── config/                 # Environment variable loader (Config struct)
│   ├── database/               # pgxpool initialisation with connection tuning
│   ├── diff/                   # Pure-function price diff engine; no I/O
│   ├── intelligence/           # Capability scoring: aggregation, lookup, benchmark → score computation
│   ├── logger/                 # zerolog setup with OTel trace_id/span_id injection
│   ├── mailer/                 # Transactional email delivery (Resend)
│   ├── metrics/                # Prometheus collectors, HTTP middleware, active-key gauge
│   ├── middleware/             # Auth, rate limiting (per-key and per-IP), response cache, security headers
│   ├── models/                 # Domain types: Model, Source, Price, PriceHistory, ReviewQueueItem
│   ├── otel/                   # OpenTelemetry SDK init with no-op fallback
│   ├── reconciler/             # Reconciliation engine: 2-source agreement, flagging, webhook fan-out
│   ├── review/                 # Admin review queue: HTTP handlers + server-rendered HTML UI
│   ├── scraper/
│   │   ├── openrouter/         # OpenRouter /v1/models pricing scraper (every 6h)
│   │   ├── litellm/            # LiteLLM GitHub JSON pricing scraper (every 24h)
│   │   ├── huggingface/        # HuggingFace pricing scraper (every 24h)
│   │   ├── openai/             # OpenAI pricing docs scraper (every 24h)
│   │   ├── anthropic/          # Anthropic pricing docs scraper (every 24h)
│   │   ├── gemini/             # Google Gemini pricing docs scraper (every 24h)
│   │   ├── swebench/           # SWE-bench Verified benchmark scraper (daily)
│   │   ├── livecodebench/      # LiveCodeBench benchmark scraper (daily)
│   │   ├── chatbot_arena/      # Disabled compatibility stub; no working public source
│   │   ├── benchmark_scraper.go # BenchmarkScraper interface
│   │   ├── ssrf.go             # Transport that blocks private/loopback/link-local addresses
│   │   └── slugmap/            # Allowlisted identity + delimited variant → canonical DB slug
│   ├── signup/                 # Signup domain: store, tokens, sessions, AbuseGuard, Unkey issuance
│   ├── webhooks/               # Webhook domain types
│   └── worker/                 # asynq task handlers: scrapers, benchmark jobs, webhook delivery
├── frontend/                   # Next.js app (llmrates.live) — SSR pages, compare, calculator, charts
├── mcp/                        # @llmrates/mcp — MCP server exposing the API as agent tools
├── migrations/                 # SQL schema migrations (golang-migrate)
├── monitoring/                 # Prometheus alert rules, Grafana dashboards, agent config
├── docs/                       # Design notes and rollout plans
├── docker-compose.yml          # Local postgres + redis
├── Makefile                    # Dev workflow targets
├── railway.json                # Railway build/deploy/health-check config
├── DEPLOY.md                   # Step-by-step Railway deployment runbook
└── .env.example                # All supported environment variables with descriptions
```

Every module carries its own `README.md` explaining its purpose, structure, key components, dependencies, and usage — each package under `internal/` and `cmd/`, plus `frontend/`, `mcp/`, `monitoring/`, and `migrations/`.

---

## API Endpoints

Every `/v1` endpoint requires an API key: `Authorization: Bearer <api-key>`. Error responses follow [RFC 7807 Problem Details](https://www.rfc-editor.org/rfc/rfc7807).

### Pricing and models

| Endpoint | Access | Description |
|---|---|---|
| `GET /v1/models` | API key | List models; filter by `?provider=`, `?modality=`, `?min_context=` |
| `GET /v1/models/:id` | API key | Single model with current prices and trust metadata |
| `GET /v1/models/:id/history` | API key | Full price history; filter by `?from=`, `?to=` |
| `GET /v1/providers` | API key | List all known providers |
| `GET /v1/changes` | API key | Recent price changes; filter by `?since=`, `?provider=` |
| `GET /v1/changes/summary` | API key | Aggregate counts and totals for recent price changes |
| `GET /v1/compare?models=slug1,slug2[&use_case=X]` | API key | Side-by-side pricing + capability scores for up to 5 models (by slug). Optional `use_case` returns rationale |
| `GET /v1/recommend` | API key | Models ranked by task type, context size, price, and benchmark capability scores; includes per-dimension `freshness` |

### Agent interface

| Endpoint | Access | Description |
|---|---|---|
| `GET /v1/context` | API key | ≤2 100-token pricing snapshot for agent system prompts |
| `POST /v1/ask` | API key | Natural-language query → structured response with `inferred_params` |
| `GET /v1/stream/changes` | API key | SSE stream of price changes; supports `Last-Event-ID` reconnection |

### Webhooks

| Endpoint | Access | Description |
|---|---|---|
| `POST /v1/webhooks` | API key | Register a webhook URL for price-change notifications |
| `DELETE /v1/webhooks/:id` | API key | Remove a webhook |

Registration accepts `https` URLs only, and rejects any URL resolving to a loopback, private,
link-local, or unspecified address (SSRF prevention). Deletion is scoped to the owning key.

**Each API key may hold at most 5 active webhooks.** A 6th registration returns `409 Conflict` with
a `max_webhooks` field in the RFC 7807 extensions. Deleting a webhook frees a slot. The cap bounds
delivery fan-out: every confirmed price change enqueues one job per active webhook.

### Signup (public, IP rate-limited)

Self-serve magic-link signup that provisions an Unkey API key. Disabled when `SIGNUP_ENABLED=false` (returns 503).

| Endpoint | Description |
|---|---|
| `POST /auth/signup/request-link` | Email a magic link |
| `GET /auth/signup/verify` | Verify the link and open a session |
| `GET /auth/signup/me` | Current session details (session required) |
| `POST /auth/signup/issue-key` | Issue the account's API key (session required) |
| `POST /auth/signup/regenerate-key` | Rotate the account's API key (session required) |

### Public and operational

| Endpoint | Access | Description |
|---|---|---|
| `GET /openapi.json` | Public | OpenAPI 3.1 spec |
| `GET /.well-known/ai-plugin.json` | Public | AI plugin manifest |
| `GET /llms.txt` | Public | Plain-text model index for LLM discovery (Redis-cached, 30 min TTL) |
| `GET /health` | Public | `{"status":"ok","db":"ok","redis":"ok"}`; returns 503 + `"degraded"` if a dependency is down |
| `GET /admin/review` | Basic auth | Review queue UI for flagged discrepancies |
| `POST /admin/review/:id/approve` · `/reject` | Basic auth | Resolve a flagged record |
| `GET /metrics` | Internal port | Prometheus scrape endpoint (`METRICS_PORT`, not exposed publicly) |

### Tiers and rate limits

**The API is free. There are no paid plans and no tier gating.** An API key is required, and every
valid key reaches every endpoint.

Keys still carry a `free` / `developer` / `pro` tier in their Unkey metadata, attached to the request
context by the auth middleware, but nothing gates on it. The only remaining effect is on the daily
counter:

| Tier | Daily requests |
|---|---|
| Free | 1 000 000 (effectively unlimited) |
| Developer | 1 000 000 (effectively unlimited) |
| Pro | Unlimited (counter skipped) |

`free` and `developer` resolve to the same ceiling, so that distinction changes nothing. The Redis
counters are retained only to track per-key usage for abuse analytics, and the tier is still used as
a Prometheus label.

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
make tidy          # go mod tidy
make install-tools # Install the golang-migrate CLI
make migrate-down  # Roll back last migration
make logs          # Stream Docker container logs
make down          # Stop Docker containers
```

---

## Environment Variables

Copy `.env.example` to `.env` before running locally. All variables are described in `.env.example`.

### Core

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `REDIS_URL` | No | `localhost:6379` | Redis connection string |
| `APP_ENV` | No | `development` | `development` / `staging` / `production` |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `LOG_LEVEL` | No | `debug` | `trace` / `debug` / `info` / `warn` / `error` |
| `METRICS_PORT` | No | `9091` | Internal Prometheus port; empty disables the metrics server |

### Auth and admin

| Variable | Required | Default | Description |
|---|---|---|---|
| `UNKEY_ROOT_KEY` | Yes | — | Unkey root key for API key verification |
| `UNKEY_API_ID` | Yes | — | Unkey API namespace ID |
| `ADMIN_USER` | No | `admin` | Basic auth username for `/admin` routes |
| `ADMIN_PASSWORD` | Yes (non-dev) | `changeme` | Basic auth password; startup fails if left default outside `development` |
| `WEBHOOK_SECRET_KEY` | No | ephemeral | 32-byte hex AES-256-GCM key for webhook secret encryption |

### Signup (magic link)

| Variable | Required | Default | Description |
|---|---|---|---|
| `SIGNUP_ENABLED` | No | `true` | Set `false` to return 503 from all signup routes |
| `RESEND_API_KEY` | Yes (API binary) | — | Resend key for magic-link email; validated in `cmd/api` |
| `EMAIL_FROM` | No | `LLMRates <noreply@llmrates.live>` | From address for signup email |
| `MAGIC_LINK_SIGNING_SECRET` | Yes (non-dev) | ephemeral in dev | Session/link signing secret; a random one is generated in `development` |
| `MAGIC_LINK_BASE_URL` | No | `https://llmrates.live` | Base URL used to build the magic link |
| `MAGIC_LINK_PATH` | No | `/signup/verify` | Path appended to the base URL |
| `MAGIC_LINK_TTL_MINUTES` | No | `15` | Magic-link validity window |
| `SIGNUP_SESSION_COOKIE_NAME` | No | `llmrates_signup` | Session cookie name |
| `SIGNUP_SESSION_TTL_HOURS` | No | `24` | Session lifetime |

The session cookie's `Secure` flag is derived from `APP_ENV` (on for everything except `development`) rather than configured directly.

### Observability

| Variable | Required | Default | Description |
|---|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OTLP gRPC endpoint; leave empty to disable tracing |
| `OTEL_SERVICE_NAME` | No | `llm-pricing-api` | Service name in traces and logs |

In production on Railway, `DATABASE_URL` and `REDIS_URL` are injected automatically when the Timescale and Redis services are linked.

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
| 000002 | `sources` table (provider rows seeded) |
| 000003 | `models` table |
| 000004 | `prices` table (current confirmed price per model+source) |
| 000005 | `price_history` TimescaleDB hypertable (7-day chunks, deduplication index) |
| 000006 | `review_queue` table |
| 000007 | `webhooks` table |
| 000008–000011 | Source additions and URL restorations (HuggingFace, OpenAI, Anthropic, Google) |
| 000012 | `api_identities`, `magic_link_tokens`, `api_keys_registry` (signup identity) |
| 000013–000016 | Benchmark catalogue, benchmark evidence, capability scores, seed evidence |
| 000017 | `prices.last_verified_at` freshness anchor |
| 000018 | Benchmark upstream provenance (`source_model_name`, `source_entry_name`, `last_observed_at`) and active-evidence lookup index |

---

## Deployment

See [`DEPLOY.md`](DEPLOY.md) for the full Railway deployment runbook, including:

- Provisioning TimescaleDB and Redis
- Running migrations against the Railway database
- Setting all required environment variables
- Generating a public domain
- Running the load test (`hey -n 10000 -c 100`)

The Go API and worker run on Railway; the frontend is deployed separately at `https://llmrates.live`.

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

The Go suite uses mock implementations of every storage and external interface (`mockStore`, `mockReconcilerStore`, `mockScraper`, `mockUnkeyVerifier`), so no live database or Redis instance is required. Frontend and MCP tests run under Vitest (`cd frontend && npm test`, `cd mcp && npm test`).

---

## Benchmark Ingestion

Two working benchmark feeds are scraped daily by background workers:

| Leaderboard | Dimensions | Normalisation |
|---|---|---|
| SWE-bench Verified | `coding`, `agentic` | Resolved percentage (0–100); best reported agent-system submission per model, stored with low base-model confidence |
| LiveCodeBench | `coding` | Mean pass@1 percentage (0–100) |

Chatbot Arena has no working public ingestion source. Its compatibility handler logs an explicit skip and is not scheduled. Seed rows for other benchmarks remain readable but are not presented as live feeds.

Both live scrapers resolve model identity through `slugmap.Resolve()`, which maps allowlisted leaderboard identities and clearly delimited date/variant suffixes to canonical DB slugs. Unknown or ambiguous generations are logged and skipped — the resolver never attaches one model generation to another.

### Capability recompute

`ComputeAllCapabilityScores()` runs synchronously after each successful live scrape; if it fails, the scrape task fails and is retried. A daily recompute acts as a safety net and refreshes freshness metadata.

Benchmark evidence is immutable: an unchanged re-scrape advances only `last_observed_at`, while a changed score or provenance appends a new record. Recompute replaces each model's scores transactionally and derives freshness from evaluation dates only. Evidence older than 90 days is marked stale; consumers decide how to treat that state.

### Manual ingestion

For one-off score insertions (e.g. internal evaluations, private benchmarks):

```bash
go run ./cmd/tools/ingest_benchmark \
  --model openai/gpt-4o \
  --benchmark "LiveCodeBench" \
  --score 91.3 \
  --source https://livecodebench.github.io/leaderboard.html
```

The CLI inserts the score and triggers a capability recompute for the affected model.
