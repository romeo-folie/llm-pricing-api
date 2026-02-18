---
name: rest-api
status: in-progress
created: 2026-02-18T12:36:55Z
updated: 2026-02-18T15:16:05Z
progress: 78%
prd: .claude/prds/rest-api.md
github: https://github.com/romeo-folie/llm-pricing-api/issues/11
---

# Epic: rest-api

## Overview

Build all nine `/v1/` REST endpoints on the existing Fiber + PostgreSQL + Redis scaffold from Phase 1. Wire OTel SDK instrumentation (otelfiber, otelgorm, otelredis) and zerolog structured logging from day one. Add Unkey API key authentication with Redis-cached validation, per-tier rate limiting, RFC 7807 error formatting, Redis response caching, and trust metadata on every response. Deliver a Railway-deployed, load-tested, integration-tested API.

The existing `cmd/api/main.go` already has Fiber, DB, Redis, graceful shutdown, and basic auth on `/admin`. All nine tasks extend this foundation — nothing is rewritten from scratch.

---

## Architecture Decisions

- **Extend, don't rewrite** — existing Fiber app, DB pool, Redis client, and graceful shutdown stay as-is. New middleware and routes are registered on top.
- **OTel SDK with no-op default** — `OTEL_EXPORTER_OTLP_ENDPOINT` env var controls the OTLP exporter. When unset, SDK defaults to a no-op provider. Zero runtime overhead until Phase 6 collector is live.
- **zerolog over slog** — zerolog's `With()` API makes it trivial to inject `trace_id` and `span_id` from the active OTel span context into every log line. The existing `slog` calls in `main.go` migrate to zerolog at bootstrap time.
- **Unkey validation cached in Redis (30s TTL)** — avoids per-request Unkey network round-trip; cache key is `unkey:{key_hash}`, never the raw key value.
- **Middleware stack order** — otelfiber → zerolog → recover → Unkey auth → rate limit → response cache → route handlers. This ensures every handler has span context, tier info, and cache state available.
- **Trust metadata computed at query time** — `confirmed_at`, `age_hours`, and `change_velocity` are derived from `price_history` rows at response time and injected into a `meta` envelope on each model.
- **Webhook delivery via asynq** — reuses the existing asynq worker scaffold from Phase 1. No new queue infrastructure needed.
- **Integration tests use real Fiber test server** — `app.Test()` with a real in-memory request/response cycle; no HTTP mocking.

---

## Technical Approach

### Middleware Stack (`internal/middleware/`)
- `otel.go` — OTel SDK init, OTLP exporter setup, shutdown hook
- `otelfiber.go` — registers otelfiber middleware; propagates span context into Fiber's `c.Locals`
- `logging.go` — zerolog request logger; extracts trace_id/span_id from span context, logs request/response
- `auth.go` — Unkey key validation, tier extraction, Redis caching (30s TTL), attaches `tier` to `c.Locals`
- `ratelimit.go` — Redis counter per key, per-day window; injects `Retry-After` on 429
- `cache.go` — Redis response caching; cache key = `{method}:{path}:{query}:{tier}`; per-endpoint TTLs from config
- `errors.go` — RFC 7807 `application/problem+json` error handler registered as Fiber's custom error handler

### API Handlers (`internal/api/handlers/`)
One file per route group:
- `models.go` — GET /v1/models, GET /v1/models/:id
- `history.go` — GET /v1/models/:id/history
- `compare.go` — GET /v1/compare
- `recommend.go` — GET /v1/recommend
- `providers.go` — GET /v1/providers
- `changes.go` — GET /v1/changes
- `context.go` — GET /v1/context
- `webhooks.go` — POST /v1/webhooks, DELETE /v1/webhooks/:id
- `sse.go` — GET /v1/stream/changes (stub)
- `discovery.go` — /openapi.json, /.well-known/ai-plugin.json, /llms.txt

### Shared API Helpers (`internal/api/`)
- `trust.go` — builds `TrustMeta` struct (`confirmed_at`, `source`, `confidence`, `age_hours`, `change_velocity`) from `price_history` query results
- `problem.go` — RFC 7807 `ProblemDetail` struct + constructors (`NewUnauthorized`, `NewForbidden`, `NewNotFound`, etc.)
- `response.go` — `JSON` helper that wraps data in `{data: ..., meta: ...}` envelope

### Config Extensions (`internal/config/config.go`)
New env vars added to existing `Config` struct:
- `UNKEY_ROOT_KEY` — Unkey API key for server-side validation
- `UNKEY_API_ID` — Unkey API namespace ID
- `OTEL_EXPORTER_OTLP_ENDPOINT` — OTLP gRPC endpoint (empty = no-op)
- `OTEL_SERVICE_NAME` — defaults to `llm-pricing-api`
- `OTEL_ENV` — defaults to `APP_ENV`

### Webhook Worker (`internal/worker/`)
- New asynq task type `TypeWebhookDeliver` registered in existing `tasks.go`
- Handler: marshal payload → HMAC-SHA256 sign → POST to registered URL → retry on non-2xx

### Infrastructure
- Railway: existing project; add `UNKEY_ROOT_KEY`, `OTEL_EXPORTER_OTLP_ENDPOINT` env vars
- Load test: `hey -n 10000 -c 100` against `/v1/models` with warm cache; target p99 < 200ms

---

## Implementation Strategy

**Build order follows dependency graph:**
1. Foundation (OTel + logging) — no deps; everything else builds on this
2. Auth + rate limiting — depends on foundation
3. Shared API layer (errors, caching, trust metadata) — depends on foundation
4. Endpoints (4 tasks, can be parallelised once 2+3 done) — depends on auth + shared layer
5. Integration tests — depends on all endpoints
6. Deploy + load test — depends on passing integration tests

**Risk mitigation:**
- Unkey latency: Redis cache with 30s TTL (implemented in task 2)
- OTLP exporter failure: SDK no-op default means startup never fails
- Railway TimescaleDB: already validated in Phase 1

---

## Tasks Created

- [ ] #12 — OTel SDK init + zerolog structured logging (parallel: false)
- [ ] #14 — Unkey auth middleware + rate limiting (parallel: true)
- [ ] #16 — Shared API layer: RFC 7807 errors, response caching, trust metadata (parallel: true)
- [ ] #18 — Free-tier endpoints: /v1/models, /v1/models/:id, /v1/providers, /v1/compare, /v1/changes (parallel: true)
- [ ] #20 — Developer+ endpoints: /v1/models/:id/history, /v1/recommend, /v1/context (parallel: true)
- [ ] #13 — Pro endpoints + webhook asynq delivery (parallel: true)
- [ ] #15 — SSE stub + discovery endpoints (parallel: true)
- [ ] #17 — Integration test suite (parallel: false)
- [ ] #19 — Railway deployment + load test (parallel: false)

Total tasks: 9
Parallel tasks: 6
Sequential tasks: 3
Estimated total effort: ~56 hours
## Dependencies

**Internal (Phase 1 deliverables — complete):**
- `internal/reconciler` — price data source for all read endpoints
- `internal/database` — existing pgx pool
- `internal/cache` — existing Redis client
- `internal/worker/tasks.go` — asynq task registration (extend for webhook delivery)
- `internal/models` — `Model`, `PriceHistory` types
- DB migrations: `models`, `price_history`, `sources`, `review_queue` — all in place

**External:**
- Unkey account + API namespace + three key tiers (Free, Developer, Pro) configured before Task 2
- Railway project with Postgres + Redis provisioned (inherited from Phase 1)
- Go modules: `go.opentelemetry.io/otel`, `github.com/gofiber/contrib/otelfiber`, `github.com/rs/zerolog`, `github.com/unkeyed/unkey-go`

---

## Success Criteria (Technical)

| Gate | Criterion |
|------|-----------|
| Endpoints | All 9 return correct data + HTTP status codes |
| Auth | Free key → 403 on Developer+/Pro endpoints |
| Rate limiting | Free key → 429 after 100 req/day |
| Trust metadata | `confirmed_at`, `source`, `confidence` present on all model responses |
| Performance | p99 < 200ms on `/v1/models` at 100 concurrent (warm cache) |
| OTel | Spans visible in Jaeger or `otel-cli` for HTTP + DB + Redis |
| Logging | Every request log line contains `trace_id` + `span_id` |
| Tests | Integration suite: 0 failures |
| Deploy | Railway deployment succeeds, health endpoint returns 200 |

---

## Estimated Effort

- **Total:** ~56 hours
- **Critical path:** Task 1 → Task 2 → Task 3 → Tasks 4–7 (parallel) → Task 8 → Task 9
- **Parallelisable:** Tasks 4, 5, 6, 7 can run simultaneously once Tasks 1–3 are complete
- **Highest risk:** Task 2 (Unkey integration — external service dependency); mitigated by Redis cache
