# internal/metrics

Prometheus instrumentation for the API and worker.

## Purpose

Declares every Prometheus metric the service exposes, provides the Fiber middleware that records per-request counters and latency histograms, and builds the internal HTTP server that serves them. Metrics are package-level variables registered on `prometheus.DefaultRegisterer`, so any package can increment them without an import cycle or dependency injection.

`cmd/api` and `cmd/worker` each serve the registry on a **separate internal HTTP server** (`METRICS_PORT`, default `9091`) so `/metrics` is never reachable through a public port. Each binary needs its own listener because Prometheus scrapes per process — the API's endpoint does not carry the worker's pipeline counters.

## Structure

```
internal/metrics/
  metrics.go      # All metric declarations (counters, histogram, gauge)
  middleware.go   # PrometheusMiddleware — per-request instrumentation
  active_keys.go  # Rolling 1-hour unique-key tracker feeding the ActiveKeys gauge
  server.go       # NewMux / NewServer — the shared internal metrics HTTP server
  server_test.go  # Tests for the listener, exposition, label names, and disabled state
  README.md       # This file
```

## Key Components

### Metrics

| Variable | Prometheus name | Type | Labels | Written by |
|---|---|---|---|---|
| `RequestsTotal` | `llm_api_requests_total` | CounterVec | `method`, `path`, `status`, `tier` | API |
| `RequestDurationSeconds` | `llm_api_request_duration_seconds` | HistogramVec | `method`, `path` | API |
| `RateLimitHitsTotal` | `llm_api_rate_limit_hits_total` | CounterVec | `tier` | API |
| `ActiveKeys` | `llm_api_active_keys` | GaugeVec | `tier` | API |
| `ErrorsTotal` | `llm_api_errors_total` | CounterVec | `method`, `path`, `error_type` | API |
| `ScraperRunsTotal` | `llm_scraper_runs_total` | CounterVec | `source`, `status` | worker |
| `ReconcilerEventsTotal` | `llm_reconciler_events_total` | CounterVec | `event_type` | worker |
| `WebhookDeliveriesTotal` | `llm_webhook_deliveries_total` | CounterVec | `status` | worker |
| `BenchmarkScrapeRunsTotal` | `llm_benchmark_scrape_runs_total` | CounterVec | `source`, `status` | worker |
| `BenchmarkScrapeDurationSeconds` | `llm_benchmark_scrape_duration_seconds` | HistogramVec | `source` | worker |
| `SlugResolutionsTotal` | `llm_slug_resolutions_total` | CounterVec | `result` | worker |
| `BenchmarkEvidenceActive` | `llm_benchmark_evidence_active` | GaugeVec | `benchmark` | worker |
| `BenchmarkEvidenceStaleRatio` | `llm_benchmark_evidence_stale_ratio` | GaugeVec | `benchmark` | worker |
| `CapabilityRecomputeDurationSeconds` | `llm_capability_recompute_duration_seconds` | Histogram | — | worker |
| `CapabilityRecomputeFailuresTotal` | `llm_capability_recompute_failures_total` | Counter | — | worker |
| `CapabilityLastRecomputeTimestampSeconds` | `llm_capability_last_recompute_timestamp_seconds` | Gauge | — | worker |
| `SourceLastSuccessTimestampSeconds` | `llm_source_last_success_timestamp_seconds` | GaugeVec | `source` | worker |
| `PricesStaleRatio` | `llm_prices_stale_ratio` | GaugeVec | `source` | worker |
| `PricesActive` | `llm_prices_active` | GaugeVec | `source` | worker |
| `PricesPublished` | `llm_prices_published` | GaugeVec | `source` | worker |
| `QueueTasks` | `llm_asynq_queue_tasks` | GaugeVec | `queue`, `state` | worker |
| `QueueLatencySeconds` | `llm_asynq_queue_latency_seconds` | GaugeVec | `queue` | worker |
| `QueuePaused` | `llm_asynq_queue_paused` | GaugeVec | `queue` | worker |
| `LogsDroppedTotal` | `llm_logs_dropped_total` | Counter | — | API, worker |

`llm_logs_dropped_total` is the one metric written by both binaries: each process runs its own OTLP log shipper, and each drops independently when its queue is full, so both endpoints carry it.

**Written by** is the binary whose `/metrics` endpoint carries the series. Prometheus scrapes per process, so a metric written only in the worker is not on the API's endpoint, and vice versa — a dashboard panel pointed at the wrong service renders empty rather than erroring.

Every label value is bounded: `path` is the registered route pattern (not the raw path), `status`, `error_type`, `result` and `state` are closed sets, and no metric carries an API key, key hash, or model id.

The pipeline and freshness metrics are written from the worker, so both binaries link this package.

### Freshness gauges

`SourceLastSuccessTimestampSeconds`, `PricesStaleRatio`, `PricesActive` and `PricesPublished` are written by `worker.FreshnessSampler`, which the worker runs on a **60-second ticker** ([`cmd/worker`](../../cmd/worker/README.md)), and `SourceLastSuccessTimestampSeconds` is also nudged eagerly at the end of a successful scrape.

- `llm_source_last_success_timestamp_seconds{source}` — Unix timestamp of the most recent verification across the source's published prices.
- `llm_prices_stale_ratio{source}` — fraction (0–1) of the source's **active** prices older than the staleness threshold (24h).
- `llm_prices_active{source}` — the ratio's denominator: prices verified inside the **activity window** (`worker.DefaultActiveWindow`, 7 days).
- `llm_prices_published{source}` — every published price for the source. The gap between it and `llm_prices_active` is the count of rows upstream has delisted or renamed — or that a scraper change orphaned.

Sources with no published prices emit **no sample** rather than a zero: a zeroed ratio would read as "perfectly fresh" and a zeroed timestamp as "verified in 1970". A source with published prices but *no* active ones (the whole feed delisted, or nothing verified for a week) publishes its timestamp and totals but omits the ratio and the active count, because there is no denominator.

> **Why the ratio is windowed (#217).** `MarkVerified` stamps only the slugs present in the latest scrape, so a model upstream delists or renames is never re-verified again and nothing prunes it. Counting every published row therefore made the ratio climb monotonically with upstream churn — OpenRouter's reached 27% with *every* stale row genuinely delisted — until it fired permanently on healthy feeds. Restricting the denominator to models verified within the activity window restores the alert's meaning. A source that stops scraping entirely still alerts: its rows stay active (and stale) for a week while the ratio climbs, and `LLMSourceFreshnessStale` fires immediately from the last-success timestamp.

> **Deviation from the issue draft.** The draft proposed labelling the ratio by `confidence`. `confidence` is derived per API response by `api.ComputeTrustMeta` and is not a column on `prices`, so a `confidence` label would require recomputing it for every row on every sample and would still not say *which feed* went quiet. The ratio is labelled by `source` instead — cheaper (one `GROUP BY`) and directly actionable.

### Job queue

Written by `worker.QueueSampler`, which the worker runs on a **60-second ticker** using an `asynq.Inspector` over the same Redis the workers use.

- `llm_asynq_queue_tasks{queue,state}` — task counts per asynq state (`pending`, `active`, `scheduled`, `retry`, `archived`, `completed`, `aggregating`).
- `llm_asynq_queue_latency_seconds{queue}` — age of the oldest pending task. **This is the backlog signal**: a handful of tasks being worked through is healthy, while a single task waiting an hour is not, and a count cannot tell those two apart.
- `llm_asynq_queue_paused{queue}` — 1 when a queue is paused, so a queue paused by an operator (or by a bug) is visible rather than merely quiet.

Unlike the freshness gauges these publish **every state including zeroes**. Absence here would be ambiguous — sampler down, or queue empty? — and the label values are bounded by asynq's own state list, so being explicit costs nothing.

The failure this exists for: a task that keeps failing retries with its asynq `Unique(24h)` lock still held, so the next enqueue of the same task type is silently deduplicated. The pipeline looks idle rather than stuck, and a deployed fix appears not to have worked — which is exactly how a scraper fix was masked for a day.

### Benchmark evidence, slug resolution, and capability scoring

These metrics cover the benchmark-evidence and capability-scoring pipeline, whose production failure mode was invisible: the scrapers ran, but only 24 of 4,078 models had any evidence and most of that evidence was months old.

- `llm_slug_resolutions_total{result}` (`SlugResolutionsTotal`) — leaderboard entries that reached `slugmap` resolution, split into `resolved`, `unknown`, and `ambiguous`. It is incremented once per leaderboard *entry* by the benchmark scrapers, not once per resolver lookup, so LiveCodeBench's two-step `model_name` → `model_repr` fallback cannot count one entry twice. This is the metric that answers whether thin coverage comes from upstream publishing few mappable models (`unknown` low, coverage low) or from the allowlist rejecting most entries (`unknown` high).
- `llm_benchmark_scrape_runs_total{source,status}` (`BenchmarkScrapeRunsTotal`) and `llm_benchmark_scrape_duration_seconds{source}` — benchmark scrape runs. They are **separate from `llm_scraper_runs_total`**: benchmark scrapers do not funnel through `worker.runPipeline`, and folding them into the price counter would give the price-scrape failure alert a source a price re-run can never fix. `ChatbotArena` is a compatibility no-op stub and deliberately records nothing; counting its no-op success would pollute the failure signal.
- `llm_benchmark_evidence_active{benchmark}` (`BenchmarkEvidenceActive`) and `llm_benchmark_evidence_stale_ratio{benchmark}` (`BenchmarkEvidenceStaleRatio`) — written by `worker.BenchmarkSampler` on a **60-second ticker**, independently of the daily benchmark scrapes.
  - *Active* mirrors `intelligence.GetActiveBenchmarkScores`: one normalised row per `(model, benchmark)`, so a re-published benchmark version does not inflate coverage.
  - The ratio is measured from `evaluated_at`, **never** `last_observed_at`. SWE-bench is re-scraped daily, so its observation time is always fresh while its newest published evaluation is months old; measuring observation time would report it as healthy while the scorer itself marks the dimension stale. The threshold is `intelligence.StalenessThresholdDays` (90 days) so the gauge can never disagree with the scorer.
  - A benchmark with no active evidence emits **no sample**: a zeroed ratio would read as "perfectly fresh", and a zeroed count is indistinguishable from "never ingested".
- `llm_capability_recompute_duration_seconds`, `llm_capability_recompute_failures_total`, `llm_capability_last_recompute_timestamp_seconds` — recorded by `intelligence.ComputeAllCapabilityScores`. Duration is observed on failure too (a slow failure is what the dashboard needs to show); failures increment the counter while the error is still returned so the triggering scrape task fails and retries; the timestamp only advances on success, so a recompute that keeps failing leaves the value receding.

The slug-resolution counter is the one signal that is *not* written by the worker's samplers: it is incremented by the `swebench` and `livecodebench` scrapers themselves, which run inside the worker process, so it is exposed by the worker's `/metrics` listener.

### `PrometheusMiddleware`

```go
func PrometheusMiddleware() fiber.Handler
```

Records `RequestsTotal` and `RequestDurationSeconds` for every request passing through the chain. It reads `tier` and `key_hash` from the Fiber locals populated by `middleware.Auth`; both are empty strings on unauthenticated routes (`/health`, discovery, `/metrics`). Only `tier` becomes a metric label — `key_hash` feeds the in-memory active-key tracker, never a series.

**Cardinality control:** the `path` label uses `c.Route().Path` — the registered pattern, e.g. `/v1/models/:id` — not `c.Path()`. Using the raw path would create one time series per model ID and blow up the metric cardinality. The same rule applies to `llm_api_rate_limit_hits_total`, which dropped its `key_hash` label for exactly this reason: one series per API key grows without bound as users sign up.

### `ObserveActiveKey`

```go
func ObserveActiveKey(tier, keyHash string)
```

Feeds the `ActiveKeys` gauge. An in-memory tracker keeps a `tier → key_hash → last seen` map over a **rolling 1-hour window**, pruned by a background goroutine started in `init()` that ticks every minute. The gauge therefore reports distinct keys seen in the last hour per tier, not a cumulative total.

## Usage

Register the middleware early in the chain, before auth, so failed requests are still counted:

```go
app.Use(middleware.Security())
app.Use(otelfiber.Middleware())
app.Use(metrics.PrometheusMiddleware())
app.Use(requestLogger(log))
app.Use(recover.New())
```

Serve the registry on its own port. `NewServer` returns `nil` when the port is
empty, so a disabled endpoint needs no special-casing at the call site:

```go
if srv := metrics.NewServer(cfg.MetricsPort); srv != nil {
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Error().Err(err).Msg("metrics server error")
        }
    }()
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = srv.Shutdown(ctx)
    }()
}
```

`NewMux()` is exported separately for tests that need the handler without a listener.

Increment a domain metric from anywhere:

```go
metrics.ScraperRunsTotal.WithLabelValues("openrouter", "success").Inc()
```

## Design Notes

- **`promauto` + default registry.** Metrics register themselves at package init, so a missing explicit registration cannot silently drop a metric. The cost is that importing this package has a side effect.
- **Freshness gauges move on a ticker, not only on scrape success.** A gauge refreshed only inside the scrape pipeline freezes at its last good value when a scraper stops running, so `time() - llm_source_last_success_timestamp_seconds` stays small and the staleness alert — the one thing the signal exists for — never fires. The worker's 60-second sampler keeps the value advancing regardless of scrape activity.
- **Key hashes only.** Nothing here stores or exports a raw API key; the tracker keys on the SHA-256 hash produced by the auth middleware, and the hash is never used as a metric label (it would be unbounded cardinality).
- **Setting `METRICS_PORT=""` disables the metrics server** without removing instrumentation — the counters still increment, nothing scrapes them.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/api` | Reads tier/key-hash locals keys shared with the auth middleware |
| `github.com/prometheus/client_golang/prometheus` | Metric types and default registry |
| `github.com/prometheus/client_golang/prometheus/promauto` | Self-registering constructors |
| `github.com/prometheus/client_golang/prometheus/promhttp` | `/metrics` exposition handler used by `NewMux` |
| `github.com/gofiber/fiber/v2` | Middleware signature and route introspection |

Alert rules and dashboards built on these metrics live in [`monitoring/`](../../monitoring/README.md).
