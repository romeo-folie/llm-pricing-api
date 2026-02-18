---
name: rest-api
description: Versioned, authenticated REST API with OTel SDK instrumentation serving all /v1/ endpoints for the LLM pricing platform
status: backlog
created: 2026-02-18T12:27:49Z
updated: 2026-02-18T12:27:49Z
---

# PRD: REST API + OTel SDK (Phase 2)

## Executive Summary

Build the versioned, authenticated REST API that exposes the Phase 1 data pipeline to external consumers. All nine `/v1/` endpoints are delivered in this phase, along with Unkey-based API key authentication, tier gating, Redis response caching, RFC 7807 error formatting, and trust metadata on every response. OTel SDK instrumentation is wired in from day one — `otelfiber`, `otelgorm`, and `otelredis` middleware drop in alongside the build so that traces are emitting before Phase 6 activates the collector infrastructure.

This is the phase that makes the product shippable as an API business.

---

## Problem Statement

Phase 1 produced a reconciled, source-attributed dataset sitting in PostgreSQL with no external access surface. Engineers, agents, and paying customers cannot reach it. Without a versioned, authenticated API:

- There is no monetisable product.
- The frontend (Phase 3) and MCP server (Phase 4) have nothing to call.
- There is no trust metadata signal for agents deciding whether to act on a price value.
- Observability is dark — no spans, no structured log correlation.

### Why now

The frontend and agent layer are blocked entirely on this phase. OTel SDK instrumentation is included here because it costs ~10 hours to wire during the initial build and would cost significantly more to retrofit across a mature codebase later. The OTLP exporter is pointed at a no-op endpoint (env-var configurable) and produces zero runtime overhead until Phase 6 brings up the collector.

---

## User Stories

### API Consumer (Developer tier)
> "As a developer building a cost-optimised LLM routing layer, I want to query `/v1/models` with provider and modality filters so that I can get a normalised, source-attributed list of current prices without scraping providers myself."

Acceptance criteria:
- `GET /v1/models?provider=openai&modality=text` returns filtered results
- Every model in the response includes `confirmed_at`, `source`, `confidence`, `age_hours`, `change_velocity`
- Response is cached in Redis; cache TTL is per-endpoint
- Invalid filter values return RFC 7807 `400 Bad Request`

### API Consumer (Free tier)
> "As a free-tier user, I want to compare up to 5 models side-by-side so that I can evaluate cost trade-offs without upgrading."

Acceptance criteria:
- `GET /v1/compare?models=openai/gpt-4o,anthropic/claude-3-5-sonnet` returns side-by-side pricing
- Requesting more than 5 models returns `400` with a clear error body
- Accessing a Developer+ endpoint with a Free key returns `403` with `tier_required` in the error body

### API Consumer (Pro tier)
> "As a Pro subscriber, I want to register a webhook so that my system is notified within 60 seconds of any price change."

Acceptance criteria:
- `POST /v1/webhooks` registers a URL and returns a webhook ID
- `DELETE /v1/webhooks/:id` removes it
- Webhook delivery is handled by an asynq job (at-least-once, 3 retries, exponential backoff)
- Only Pro-tier keys may register webhooks; Free/Developer keys return `403`

### Platform Operator
> "As an operator, I want every log line to contain `trace_id` and `span_id` so that I can correlate a slow request in Grafana with the specific log lines it generated."

Acceptance criteria:
- All structured log output uses zerolog
- Every log line emitted during request handling includes `trace_id` and `span_id` fields extracted from the active OTel span context
- OTel SDK emits spans for HTTP requests, DB queries, and Redis commands — verifiable with local Jaeger or `otel-cli`

### Agent / MCP Consumer
> "As an AI agent, I want a pricing snapshot endpoint that fits within 2,100 tokens so that I can load current pricing data into my system prompt without blowing my context budget."

Acceptance criteria:
- `GET /v1/context` returns a structured JSON snapshot verified ≤ 2,100 tokens (tiktoken)
- Response includes trust metadata for all included values
- Requires Developer+ tier

---

## Requirements

### Functional Requirements

#### F1 — Project Structure & Config
- Go + Fiber project structure in `cmd/api/`
- All config via environment variables, loaded through `internal/config`
- Graceful shutdown on SIGTERM/SIGINT

#### F2 — OTel SDK Instrumentation
- OTel SDK initialised at startup with service name `llm-pricing-api` and resource attributes (version, environment)
- OTLP gRPC exporter endpoint configurable via `OTEL_EXPORTER_OTLP_ENDPOINT` env var; defaults to a no-op SDK when unset
- `otelfiber` middleware: auto-span per HTTP request with route, method, status code attributes
- `otelgorm` / pgx OTel hook: auto-span per DB query with table and operation attributes
- `otelredis` instrumentation: auto-span per Redis command
- No manual span creation required in business logic

#### F3 — Structured Logging
- `zerolog` (or `slog`) for all structured logging
- Every log line emitted within a request handler includes `trace_id` and `span_id` extracted from the active span context
- Log fields: `level`, `time`, `service`, `trace_id`, `span_id`, `msg`, and any handler-specific fields
- JSON format in production, pretty-print in development (env-var toggle)

#### F4 — Authentication & Tier Gating (Unkey)
- Unkey middleware validates API key on every request
- Extracts tier (`free`, `developer`, `pro`) from Unkey key metadata and attaches it to Fiber's request context
- Unkey validation result cached in Redis with 30s TTL to eliminate per-request Unkey latency
- Missing or invalid key returns `401`; insufficient tier returns `403` with `tier_required` field

#### F5 — Rate Limiting
- Free tier: 100 requests/day; excess returns `429` with `Retry-After` header
- Developer tier: 10,000 requests/day
- Pro tier: unlimited
- Rate limit counts tracked in Redis per API key

#### F6 — REST Endpoints

| Endpoint | Tier | Notes |
|----------|------|-------|
| `GET /v1/models` | Free | Filters: `?provider=`, `?modality=`, `?min_context=`; paginated |
| `GET /v1/models/:id` | Free | Single model detail |
| `GET /v1/models/:id/history` | Developer+ | Price history; filters: `?from=`, `?to=` |
| `GET /v1/compare?models=` | Free | Up to 5 models |
| `GET /v1/recommend` | Developer+ | Ranked by task, context, price |
| `GET /v1/providers` | Free | Provider list |
| `GET /v1/changes` | Free | Recent price changes; filters: `?since=`, `?provider=` |
| `POST /v1/webhooks` | Pro | Register webhook; returns webhook ID |
| `DELETE /v1/webhooks/:id` | Pro | Remove webhook |
| `GET /v1/context` | Developer+ | ≤2,100 token pricing snapshot |

#### F7 — Trust Metadata
Every response body includes a `meta` object (or per-model inline) with:
- `confirmed_at` — ISO 8601 timestamp of last confirmed price
- `source` — which source(s) confirmed the value
- `confidence` — `high`, `medium`, or `low`
- `age_hours` — hours since last confirmation
- `change_velocity` — price change rate (changes per 30 days)

#### F8 — Response Caching
- Redis response caching for all read endpoints
- Per-endpoint TTLs:
  - `/v1/models`, `/v1/providers`, `/v1/changes`: 5 minutes
  - `/v1/models/:id`: 5 minutes
  - `/v1/compare`: 2 minutes
  - `/v1/context`: 10 minutes
  - `/v1/models/:id/history`, `/v1/recommend`: no cache (dynamic)
- Cache key includes endpoint path + query params + API key tier (not the key itself)
- Cache-Control headers set on all responses

#### F9 — Error Format (RFC 7807)
All errors return `application/problem+json` with fields:
- `type` — URI identifying the error type
- `title` — human-readable summary
- `status` — HTTP status code
- `detail` — specific error detail
- `instance` — request path (optional)
- Additional fields as needed (e.g. `tier_required`, `retry_after`)

#### F10 — Webhook Delivery
- Webhook jobs enqueued via asynq on every confirmed price change
- At-least-once delivery: 3 retries with exponential backoff (max 15 minutes total)
- Payload includes: `model_id`, `provider`, `old_price`, `new_price`, `confirmed_at`, `source`
- HMAC-SHA256 signature in `X-LLMPricing-Signature` header

#### F11 — SSE Stream Placeholder
- `GET /v1/stream/changes` stub returning `200` with proper SSE headers
- `active_sse_connections` OTel gauge metric placeholder (real SSE implemented in Phase 4)

#### F12 — Discovery Endpoints
- `GET /openapi.json` — OpenAPI 3.1 spec
- `GET /.well-known/ai-plugin.json` — AI plugin manifest
- `GET /llms.txt` — plain-text model listing for agents

### Non-Functional Requirements

#### N1 — Performance
- p99 latency < 200ms for all read endpoints with warm Redis cache
- Measured under 100 concurrent requests (load test exit criterion)

#### N2 — Observability
- OTel spans emitting for HTTP, DB, and Redis — verifiable with local Jaeger or `otel-cli`
- `trace_id` + `span_id` on every log line
- Structured JSON logs in production

#### N3 — Security
- No API key value ever written to logs or cached in plaintext (cache on key hash)
- Webhook HMAC signature on every delivery
- `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` on all responses

#### N4 — Testability
- Integration test suite covers: auth (missing key, invalid key, correct key), tier gating (Free → 403 on gated endpoints), filtering, pagination, RFC 7807 error bodies, trust metadata presence
- All tests use real Fiber test server; no HTTP mocking

#### N5 — Reliability
- Graceful shutdown drains in-flight requests
- Unkey cache prevents cascading failure if Unkey is temporarily unavailable
- asynq retries handle transient webhook delivery failures

---

## Success Criteria

| Metric | Target |
|--------|--------|
| All 9 endpoints return correct data + HTTP codes | 100% |
| Free tier key returns 403 on history/recommend/webhook endpoints | 100% |
| Rate limiting: Free tier returns 429 after 100 req/day | Verified |
| All responses include confirmed_at, source, confidence | 100% |
| p99 < 200ms on /v1/models with warm cache (100 concurrent) | Pass |
| OTel SDK emitting spans (Jaeger or otel-cli verification) | Pass |
| Every log line contains trace_id + span_id | 100% |
| Integration test suite passes with 0 failures | Pass |
| Railway deployment succeeds with no runtime errors | Pass |

---

## Constraints & Assumptions

- **Phase 1 complete:** All scrapers, reconciliation engine, and price data in PostgreSQL are production-ready.
- **OTel collector not yet running:** OTLP exporter targets a no-op endpoint. SDK defaults to no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset — zero runtime impact.
- **No frontend in this phase:** API is tested via integration tests and curl/Postman only.
- **Unkey for key management:** Unkey handles key creation, validation, and tier metadata. We do not build our own key store.
- **Lemon Squeezy deferred:** Billing and paid tier key provisioning is Phase 5. For Phase 2, Developer and Pro tier keys are created manually in Unkey for testing.
- **SSE stream is a stub:** Full SSE implementation is Phase 4. The endpoint must exist and respond correctly; reconnection logic is not required yet.
- **Provider doc scrapers removed:** The refactor in commit `2bf0517` dropped HTML scrapers in favour of OpenRouter + LiteLLM. The API reflects this — no provider-doc-sourced confidence distinction needed.
- **Railway deployment:** API is deployed to Railway at the end of this phase. CI/CD pipeline is configured during this phase.

---

## Out of Scope

- Frontend (Next.js) — Phase 3
- MCP server, `/v1/ask`, full SSE implementation — Phase 4
- Lemon Squeezy billing, paid key provisioning — Phase 5
- OTel Collector, Prometheus, Loki, Tempo, Grafana — Phase 6
- Admin dashboard — Phase 7
- Load testing beyond 100 concurrent (500-concurrent test is Phase 8)
- `/v1/models/:id/history` backfill (data available from Phase 1 forward only)
- OAuth or session-based auth (API keys only)

---

## Dependencies

### Internal
- Phase 1 complete: `internal/reconciler`, `internal/diff`, `internal/scraper`, `internal/review`, all DB migrations, asynq worker scaffold
- `internal/config` — existing config management
- `internal/database` — existing DB connection pool
- `internal/cache` — existing Redis client

### External
- **Unkey** — API key management SaaS; account + namespace + key tiers must be configured before F4 can be implemented
- **Railway** — deployment target; Railway project must exist with PostgreSQL + TimescaleDB + Redis add-ons provisioned
- **OpenTelemetry Go SDK** — `go.opentelemetry.io/otel` + contrib packages
- **Fiber** — `github.com/gofiber/fiber/v2`
- **zerolog** — `github.com/rs/zerolog`
- **otelfiber** — `github.com/gofiber/contrib/otelfiber`

---

## Task Breakdown

| Task | Package | Estimate |
|------|---------|----------|
| Fiber app bootstrap + graceful shutdown + env config | `cmd/api` | 2h |
| OTel SDK init + OTLP exporter config (no-op default) | `internal/otel` | 2h |
| `otelfiber` middleware + `otelgorm` hook + `otelredis` | `internal/middleware` | 2h |
| zerolog setup with trace_id/span_id injection | `internal/logger` | 2h |
| Unkey middleware: validate, extract tier, Redis cache | `internal/middleware` | 4h |
| Rate limiting middleware (Redis counters per key) | `internal/middleware` | 2h |
| RFC 7807 error handler + problem JSON helpers | `internal/api` | 2h |
| Redis response caching middleware + per-endpoint TTLs | `internal/middleware` | 3h |
| Trust metadata helpers (confirmed_at, confidence, age_hours, change_velocity) | `internal/api` | 3h |
| `GET /v1/models` + `GET /v1/models/:id` (with filters + pagination) | `internal/api/handlers` | 4h |
| `GET /v1/models/:id/history` | `internal/api/handlers` | 2h |
| `GET /v1/compare` | `internal/api/handlers` | 2h |
| `GET /v1/recommend` | `internal/api/handlers` | 3h |
| `GET /v1/providers` + `GET /v1/changes` | `internal/api/handlers` | 2h |
| `GET /v1/context` (≤2100 token snapshot) | `internal/api/handlers` | 3h |
| `POST /v1/webhooks` + `DELETE /v1/webhooks/:id` + asynq delivery | `internal/api/handlers`, `internal/worker` | 4h |
| `GET /v1/stream/changes` stub + active_sse_connections gauge | `internal/api/handlers` | 1h |
| OpenAPI 3.1, ai-plugin.json, llms.txt discovery endpoints | `cmd/api` | 2h |
| Integration tests (auth, tier gating, filtering, RFC 7807, trust metadata) | `internal/api` | 6h |
| Railway deployment pipeline + env var configuration | `.railway/` | 2h |
| Load test: p99 < 200ms at 100 concurrent (k6 or hey) | — | 3h |

**Total:** ~56h coding
