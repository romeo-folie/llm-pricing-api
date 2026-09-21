# cmd/worker

Background job worker entrypoint for the LLM Pricing Platform.

## Purpose

Runs an asynq worker server that processes asynchronous scraper tasks from the Redis-backed job queue. On startup it connects to PostgreSQL, wires the scraper task handlers through the diff and reconciliation pipeline, and starts a cron scheduler that enqueues scrape jobs at the configured intervals. The worker is the sole process responsible for keeping pricing data fresh. It also runs a minimal HTTP health server on `APP_PORT` (default 8080) so that Railway's health check can verify the service is alive, and an internal Prometheus metrics server on `METRICS_PORT` (default 9091) that exposes the pipeline counters it produces.

## Structure

```
cmd/worker/
  main.go    # Worker entrypoint — DB, Redis, asynq server, handler registration, cron scheduler
  README.md  # This file
```

## Key Components

- **`main()`** — Loads `.env` via godotenv, reads config, opens a PostgreSQL connection pool with 5-attempt retry (matching `cmd/api`), creates an asynq server (concurrency 10), registers scraper handlers on the `ServeMux`, wires a cron scheduler with per-source intervals, starts a minimal HTTP health server on `APP_PORT`, starts the metrics server on `METRICS_PORT`, starts the freshness, benchmark-evidence and queue samplers on 60-second tickers, and blocks until `SIGINT`/`SIGTERM` triggers graceful shutdown of both servers, all three samplers, the asynq server, and the scheduler.
- **`GET /health`** — Pings both PostgreSQL and Redis. Returns `{"status":"ok","db":"ok","redis":"ok"}` (200) when healthy, or `{"status":"degraded"}` (503) when either dependency is unreachable. Used by Railway's health check to verify the worker is running.
- **`GET /metrics`** — Prometheus exposition of the process registry, served on `METRICS_PORT` by a dedicated listener that is never publicly exposed. See [Metrics](#metrics) below.

## Metrics

The worker is the **only** producer of the pipeline counters — `llm_scraper_runs_total`,
`llm_reconciler_events_total`, and `llm_webhook_deliveries_total` are all incremented inside this
process. Before this endpoint existed those samples were written to a process-local registry that
nothing read, which left the data-pipeline dashboard permanently empty and made the
`LLMScraperFailureConsecutive` alert impossible to fire.

| Setting | Behaviour |
|---|---|
| `METRICS_PORT` set (default `9091`) | Metrics server listens on that port; `GET /metrics` returns the exposition |
| `METRICS_PORT=""` | Metrics server is not started, matching `cmd/api` |

The server is built by `metrics.NewServer` in [`internal/metrics`](../../internal/metrics/README.md),
shared with `cmd/api`, and deliberately serves **only** `/metrics` — it is a telemetry surface, not a
router. A bind failure is logged at error level but is not fatal: losing telemetry must not take the
data pipeline down with it.

Because Prometheus scrapes each process separately, the API's endpoint does **not** include these
counters. A collector must scrape the worker independently:

```
# Local (see the Makefile, which offsets the worker to avoid colliding with the API)
http://localhost:9092/metrics

# Production (Railway private networking) — substitute the actual service name
http://llm-pricing-worker.railway.internal:<METRICS_PORT>/metrics
```

A `CounterVec` emits no samples until its first child is observed, so immediately after boot the
exposition contains only `go_*` and `process_*` families. The pipeline counters appear as soon as the
startup scrape tasks record their first result.

### Freshness sampler

The worker also publishes the data-freshness gauges — `llm_source_last_success_timestamp_seconds`,
`llm_prices_stale_ratio`, `llm_prices_active` and `llm_prices_published`, all labelled by `source`.
`worker.FreshnessSampler` runs them on a **60-second ticker** with a **10-second per-sample timeout**,
seeded once at boot and stopped during graceful shutdown before the metrics listener.

The stale ratio's denominator is the **active** count — prices verified inside
`worker.DefaultActiveWindow` (7 days) — not every published row. Rows upstream has delisted or renamed
are never re-verified by design, so counting them made the ratio climb with upstream churn until it
fired permanently on healthy feeds ([#217](https://github.com/romeo-folie/llm-pricing-api/issues/217));
`llm_prices_published` minus `llm_prices_active` is that excluded population.

The ticker is the design, not an implementation detail. If the gauges were refreshed only inside the
scrape pipeline, a scraper that silently stopped running would leave them frozen at their last good
values: `time() - llm_source_last_success_timestamp_seconds` would stay small and the
`LLMSourceFreshnessStale` critical alert could never fire — failing at exactly the outage it exists to
detect. A sample failure is logged at warn level and never returns an error up the call stack, because
freshness telemetry must not take the data pipeline down with it.

`llm_source_last_success_timestamp_seconds` is also nudged at the end of each successful scrape
(`internal/worker.runPipeline`) so the alert has a prompt anchor, but the ticker remains the
authority.

### Benchmark-evidence sampler

`worker.BenchmarkSampler` publishes `llm_benchmark_evidence_active{benchmark}` and
`llm_benchmark_evidence_stale_ratio{benchmark}` on the **same 60-second ticker / 10-second timeout**
pattern, seeded once at boot and stopped during graceful shutdown before the metrics listener.

It must also run outside the scrape pipeline, for the opposite reason to the freshness sampler:
benchmark scrapes are **daily**, so a gauge refreshed only on scrape success would not notice
evidence crossing the 90-day staleness threshold between runs. The ratio is measured from
`evaluated_at`, not `last_observed_at` — SWE-bench is re-scraped daily and looks freshly observed
while its newest published evaluation is months old.

The benchmark scrape counters (`llm_benchmark_scrape_runs_total{source,status}`,
`llm_benchmark_scrape_duration_seconds{source}`) and the slug-resolution counter
(`llm_slug_resolutions_total{result}`) are incremented by the asynq handlers rather than by the
samplers, so they appear when benchmark tasks run rather than on a timer.

### Tracing and logs

Unlike the API, the worker historically initialised **no OTel at all**, so
scraper and reconciler spans were never exported while the API's were (#198).
It now calls `otel.Init` with the same config as `cmd/api` and shuts the
provider down on exit, so worker spans reach Tempo.

The worker also ships structured logs to Loki through the same OTLP endpoint
(#199): `logger.NewOTLPWriter` tees stdout into an `otlploggrpc` exporter, with
info-and-above shipped and debug left on stdout. `MinLevel` defaults to
`InfoLevel`; the writer never blocks and counts drops in
`llm_logs_dropped_total`.

### Queue sampler

`worker.QueueSampler` publishes `llm_asynq_queue_tasks{queue,state}`,
`llm_asynq_queue_latency_seconds{queue}` and `llm_asynq_queue_paused{queue}` on the **same
60-second ticker**, from an `asynq.Inspector` over the same Redis. It is the only signal that shows
work *stuck* rather than failed: a task that keeps retrying holds its asynq `Unique(24h)` lock, so
the next enqueue of that task type is silently deduplicated and the pipeline looks idle.

It publishes every state including zeroes — absence would be ambiguous — and unlike the SQL
samplers it takes no context, because `asynq.Inspector` has no context-aware API.

## Tasks and Cron Schedule

| Task constant | Task type string | Scraper | Schedule |
|---|---|---|---|
| `TaskOpenRouterScrape` | `scrape:openrouter` | OpenRouter API | Every 6 hours |
| `TaskLiteLLMScrape` | `scrape:litellm` | LiteLLM GitHub JSON | Every 24 hours |
| `TaskHuggingFaceScrape` | `scrape:huggingface` | HuggingFace Inference Providers | Every 24 hours |
| `TaskOpenAIScrape` | `scrape:openai` | OpenAI pricing page | Every 24 hours |
| `TaskAnthropicScrape` | `scrape:anthropic` | Anthropic pricing page | Every 24 hours |
| `TaskGeminiScrape` | `scrape:gemini` | Google Gemini pricing page | Every 24 hours |
| `TaskSWEBenchScrape` | `benchmark:swebench` | SWE-bench Verified leaderboard | Every 24 hours |
| `TaskLiveCodeBenchScrape` | `benchmark:livecodebench` | LiveCodeBench leaderboard | Every 24 hours |
| `TaskChatbotArenaScrape` | `benchmark:chatbot_arena` | Chatbot Arena (no-op compatibility stub) | on-demand only |
| `TaskRecomputeCapabilityScores` | `intelligence:recompute_capability_scores` | Capability recompute (daily safety net) | Every 24 hours |
| `TaskStalenessCheck` | `intelligence:staleness_check` | Legacy alias of the recompute handler | legacy |
| `TaskBFCLScrapeDeprecated` | `benchmark:bfcl` | Pre-rename queue drain into the SWE-bench handler | legacy |
| `TaskHuggingFaceLLMScrapeDeprecated` | `benchmark:huggingface_llm` | Pre-rename queue drain into the LiveCodeBench handler | legacy |

## Pipeline

Each price handler executes the same three-stage pipeline:

1. **Scrape** — the handler instantiates its scraper (e.g. `openrouter.New(nil)`) and calls `Fetch(ctx)` to retrieve the latest `[]models.ScrapedModel` from the remote source.
2. **Diff** — `diff.Diff(storedPrices, storedModels, scraped)` compares the incoming data against the values currently stored in PostgreSQL, producing a list of price changes.
3. **Reconcile** — `reconciler.Reconcile(ctx, diffs)` applies the reconciliation rules: single-source changes are queued for a second confirming fetch; multi-source disagreements >5% are flagged to the review queue; confirmed changes are written as immutable records in `price_history`.

Benchmark handlers do **not** run that pipeline: they fetch a leaderboard, resolve each entry's model name to a canonical slug, upsert immutable evidence, and then synchronously recompute capability scores. The recompute reuses the same transactional replacement path as the daily `TaskRecomputeCapabilityScores` safety net, so a failure fails the scrape task and asynq retries it.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/config` | Loads environment variables into a `Config` struct |
| `internal/database` | Opens and pings the PostgreSQL connection pool |
| `internal/worker` | `WorkerStore`, `Handlers`, and task name constants |
| `internal/reconciler` | Mediates all writes to `price_history` |
| `internal/metrics` | Pipeline counters plus the shared `/metrics` HTTP server (`metrics.NewServer`) |
| `github.com/hibiken/asynq` | Distributed task queue and cron scheduler backed by Redis |
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pool |
| `github.com/joho/godotenv` | `.env` file loading |

## Usage

```bash
# Preferred: `make worker` offsets APP_PORT and METRICS_PORT so the worker can
# run alongside `make run` without either service failing to bind.
make worker

# Run directly — set both ports yourself, or the worker collides with a running
# API on :8080 and exits non-zero when its health listener fails to bind.
APP_PORT=8081 METRICS_PORT=9092 go run ./cmd/worker

# Build and run
go build -o bin/worker ./cmd/worker
APP_PORT=8081 METRICS_PORT=9092 ./bin/worker
```

Requires `DATABASE_URL` and `REDIS_URL` environment variables (and optionally `APP_ENV`). `APP_PORT`
and `METRICS_PORT` default to `8080` and `9091`; set `METRICS_PORT=""` to disable the metrics
endpoint. See `.env.example`.

Note that `make worker` always sets `METRICS_PORT`, overriding a blank value from `.env`. To disable
metrics under that target, override the Makefile variable: `make worker WORKER_METRICS_PORT=""`.
