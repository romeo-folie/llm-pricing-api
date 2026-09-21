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

| Variable | Prometheus name | Type | Labels |
|---|---|---|---|
| `RequestsTotal` | `llm_api_requests_total` | CounterVec | `method`, `path`, `status`, `tier` |
| `RequestDurationSeconds` | `llm_api_request_duration_seconds` | HistogramVec | `method`, `path` |
| `RateLimitHitsTotal` | `llm_api_rate_limit_hits_total` | CounterVec | tier |
| `ActiveKeys` | `llm_api_active_keys` | GaugeVec | `tier` |
| `ErrorsTotal` | `llm_api_errors_total` | CounterVec | error classification |
| `ScraperRunsTotal` | `llm_scraper_runs_total` | CounterVec | scraper, outcome |
| `ReconcilerEventsTotal` | `llm_reconciler_events_total` | CounterVec | event kind |
| `WebhookDeliveriesTotal` | `llm_webhook_deliveries_total` | CounterVec | delivery outcome |
| `SourceLastSuccessTimestampSeconds` | `llm_source_last_success_timestamp_seconds` | GaugeVec | `source` |
| `PricesStaleRatio` | `llm_prices_stale_ratio` | GaugeVec | `source` |
| `PricesPublished` | `llm_prices_published` | GaugeVec | `source` |

The pipeline and freshness metrics are written from the worker, so both binaries link this package.

### Freshness gauges

`SourceLastSuccessTimestampSeconds`, `PricesStaleRatio` and `PricesPublished` are written by `worker.FreshnessSampler`, which the worker runs on a **60-second ticker** ([`cmd/worker`](../../cmd/worker/README.md)), and `SourceLastSuccessTimestampSeconds` is also nudged eagerly at the end of a successful scrape.

- `llm_source_last_success_timestamp_seconds{source}` — Unix timestamp of the most recent verification across the source's published prices.
- `llm_prices_stale_ratio{source}` — fraction (0–1) of the source's published prices older than the staleness threshold (24h).
- `llm_prices_published{source}` — number of published prices for the source, so the ratio has absolute context.

Sources with no published prices emit **no sample** rather than a zero: a zeroed ratio would read as "perfectly fresh" and a zeroed timestamp as "verified in 1970".

> **Deviation from the issue draft.** The draft proposed labelling the ratio by `confidence`. `confidence` is derived per API response by `api.ComputeTrustMeta` and is not a column on `prices`, so a `confidence` label would require recomputing it for every row on every sample and would still not say *which feed* went quiet. The ratio is labelled by `source` instead — cheaper (one `GROUP BY`) and directly actionable.

### `PrometheusMiddleware`

```go
func PrometheusMiddleware() fiber.Handler
```

Records `RequestsTotal` and `RequestDurationSeconds` for every request passing through the chain. It reads `tier` and `key_hash` from the Fiber locals populated by `middleware.Auth`; both are empty strings on unauthenticated routes (`/health`, discovery, `/metrics`).

**Cardinality control:** the `path` label uses `c.Route().Path` — the registered pattern, e.g. `/v1/models/:id` — not `c.Path()`. Using the raw path would create one time series per model ID and blow up the metric cardinality.

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
