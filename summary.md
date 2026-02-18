# Phase: rest-api
> Build all 9 /v1/ REST endpoints with OTel SDK instrumentation, Unkey auth, tier gating, Redis caching, and trust metadata. Deploy to Railway.

**Branch:** `epic/rest-api` | **Worktree:** `../epic-rest-api` | **Created:** 2026-02-18T12:52:51Z

---

## Current Status

| # | Task | Status | GitHub |
|---|------|--------|--------|
| #12 | OTel SDK init + zerolog structured logging | done | [#12](https://github.com/romeo-folie/llm-pricing-api/issues/12) |
| #14 | Unkey auth middleware + rate limiting | done | [#14](https://github.com/romeo-folie/llm-pricing-api/issues/14) |
| #16 | Shared API layer (RFC 7807, caching, trust metadata) | done | [#16](https://github.com/romeo-folie/llm-pricing-api/issues/16) |
| #18 | Free-tier endpoints | done | [#18](https://github.com/romeo-folie/llm-pricing-api/issues/18) |
| #20 | Developer+ endpoints | done | [#20](https://github.com/romeo-folie/llm-pricing-api/issues/20) |
| #13 | Pro endpoints + webhook asynq delivery | done | [#13](https://github.com/romeo-folie/llm-pricing-api/issues/13) |
| #15 | SSE stub + discovery endpoints | done | [#15](https://github.com/romeo-folie/llm-pricing-api/issues/15) |
| #17 | Integration test suite | open | [#17](https://github.com/romeo-folie/llm-pricing-api/issues/17) |
| #19 | Railway deployment + load test | open (blocked by #17) | [#19](https://github.com/romeo-folie/llm-pricing-api/issues/19) |

**Next action:** Run `/pm:issue-start 17` — integration test suite is now unblocked.

---

## Goals

- Wire OTel SDK (otelfiber + otelgorm + otelredis) from day one; OTLP exporter defaults to no-op until Phase 6
- Replace slog with zerolog; inject trace_id + span_id on every log line
- Unkey API key authentication with Redis-cached validation (30s TTL)
- Per-tier rate limiting (Free: 100/day, Developer: 10k/day, Pro: unlimited)
- All 9 /v1/ endpoints with trust metadata on every response
- RFC 7807 error format throughout
- Redis response caching with per-endpoint TTLs
- Webhook delivery via asynq (HMAC-SHA256 signed, 3 retries)
- Integration test suite with 0 failures
- Railway-deployed, p99 < 200ms at 100 concurrent on /v1/models

---

## Architecture Notes

- **Extend, don't rewrite** — existing Fiber app, pgx pool, Redis client, and graceful shutdown in `cmd/api/main.go` stay as-is; new middleware and routes registered on top
- **Middleware stack order** — otelfiber → zerolog → recover → Unkey auth → rate limit → response cache → handlers; this order ensures span context is available everywhere
- **OTel SDK with no-op default** — `OTEL_EXPORTER_OTLP_ENDPOINT` env var controls the OTLP exporter; when unset, SDK is a no-op; zero startup risk
- **Unkey cache key** — `unkey:{sha256(raw_key)}` stored in Redis; raw key value never written to Redis or logs
- **Trust metadata computed at query time** — `TrustMeta` struct populated from `price_history` rows fetched by the handler; confidence rule: high = 2+ sources agree, medium = single source <24h, low = single source ≥24h
- **Webhook delivery** — reuses existing asynq worker from Phase 1; new task type `TypeWebhookDeliver` registered in `internal/worker/tasks.go`
- **Scrapers** — Phase 1 refactor dropped provider HTML scrapers; only OpenRouter + LiteLLM sources in production

---

## Key Files

| Path | Role |
|------|------|
| `cmd/api/main.go` | API entrypoint — extend with OTel init, middleware registration, /v1/ route group |
| `internal/config/config.go` | Config — add UNKEY_ROOT_KEY, UNKEY_API_ID, OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME |
| `internal/otel/` | New — OTel SDK init, OTLP exporter, shutdown hook |
| `internal/logger/` | New — zerolog setup with trace_id/span_id injection |
| `internal/middleware/` | New — auth.go, ratelimit.go, cache.go, security.go |
| `internal/api/` | New — problem.go (RFC 7807), trust.go, response.go |
| `internal/api/handlers/` | New — one file per route group |
| `internal/worker/tasks.go` | Extend — register TypeWebhookDeliver task |
| `internal/reconciler/reconciler.go` | Extend — enqueue webhook delivery on confirmed price change |
| `migrations/` | May need webhooks table migration |

---

## CCPM Quick Commands

```text
/pm:next                    # What to work on next
/pm:issue-start 12          # Begin the foundation task (start here)
/pm:issue-close <N>         # Mark a task complete
/pm:status                  # Full project dashboard
/pm:epic-status rest-api    # This epic's progress
/pm:blocked                 # See blocked tasks
```

---

## Resuming Work

1. Check **Current Status** table above for the next `open` task without blockers.
2. Run `/pm:issue-start <N>` to claim it and read its full spec.
3. Write tests before implementation (TDD).
4. Run `go test ./...` — all tests must pass before committing.
5. Run `/code-reviewer` — fix all findings before closing.
6. Run `/pm:issue-close <N>` to mark complete and update the status table in this file.
7. The next unblocked task is always #12 first, then #14 and #16 in parallel, then #18/#20/#13/#15 in parallel, then #17, then #19.
