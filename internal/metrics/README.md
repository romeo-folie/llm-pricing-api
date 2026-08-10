# internal/metrics

Prometheus instrumentation for the API and worker.

## Purpose

Declares every Prometheus metric the service exposes and provides the Fiber middleware that records per-request counters and latency histograms. Metrics are package-level variables registered on `prometheus.DefaultRegisterer`, so any package can increment them without an import cycle or dependency injection.

`cmd/api` serves these on a **separate internal HTTP server** (`METRICS_PORT`, default `9091`) so `/metrics` is never reachable through the public API port.

## Structure

```
internal/metrics/
  metrics.go      # All metric declarations (counters, histogram, gauge)
  middleware.go   # PrometheusMiddleware — per-request instrumentation
  active_keys.go  # Rolling 1-hour unique-key tracker feeding the ActiveKeys gauge
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

The last three are incremented from the worker, so both binaries link this package.

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

Serve the registry on its own port:

```go
metricsMux := http.NewServeMux()
metricsMux.Handle("/metrics", promhttp.Handler())
// listened on :METRICS_PORT, separate from the Fiber app
```

Increment a domain metric from anywhere:

```go
metrics.ScraperRunsTotal.WithLabelValues("openrouter", "success").Inc()
```

## Design Notes

- **`promauto` + default registry.** Metrics register themselves at package init, so a missing explicit registration cannot silently drop a metric. The cost is that importing this package has a side effect.
- **Key hashes only.** Nothing here stores or exports a raw API key; the tracker keys on the SHA-256 hash produced by the auth middleware, and the hash is never used as a metric label (it would be unbounded cardinality).
- **Setting `METRICS_PORT=""` disables the metrics server** without removing instrumentation — the counters still increment, nothing scrapes them.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/api` | Reads tier/key-hash locals keys shared with the auth middleware |
| `github.com/prometheus/client_golang/prometheus` | Metric types and default registry |
| `github.com/prometheus/client_golang/prometheus/promauto` | Self-registering constructors |
| `github.com/gofiber/fiber/v2` | Middleware signature and route introspection |

Alert rules and dashboards built on these metrics live in [`monitoring/`](../../monitoring/README.md).
