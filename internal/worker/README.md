# internal/worker

Asynq task constants, DB read layer, handler functions, and webhook delivery job for the LLM Pricing Platform's background worker pipeline.

## Purpose

This package is the bridge between the asynq job queue and the scrape→diff→reconcile data pipeline. It defines:

- **Task name constants** used when registering handlers and scheduling cron jobs.
- **`WorkerStore`** — a DB read interface (and its pgx implementation) that supplies the diff engine with the current stored models and prices for each source.
- **`Handlers`** — one public handler method per data source, each executing the full pipeline: fetch scraped data, fetch stored data, compute diffs, reconcile.
- **`FreshnessSampler`** — a per-source price-freshness query that publishes the last-success timestamp and stale-ratio gauges; run on a ticker by `cmd/worker` so a scraper that stops running is still detected.
- **Webhook delivery** — `HandleWebhookDeliver` processes `webhook:deliver` asynq tasks, signs the payload with HMAC-SHA256, and POSTs to the registered URL with retry on failure.

`cmd/worker/main.go` instantiates this package and wires it into the asynq server and cron scheduler.

## Structure

```
internal/worker/
  tasks.go              # String constants for asynq task names (including TypeWebhookDeliver)
  store.go              # WorkerStore interface + pgxWorkerStore implementation
  handlers.go           # Handlers struct, runPipeline helper, scraper handler methods
  freshness.go          # FreshnessSampler — per-source freshness query + gauges
  webhook_handler.go    # WebhookPayload, WebhookTaskPayload, NewWebhookDeliverTask, HandleWebhookDeliver
  handlers_test.go      # Unit tests using mock store and mock scraper
  freshness_test.go     # Unit tests for the sampler using a mock querier
  webhook_handler_test.go # Unit tests for HMAC signing and non-2xx retry
  README.md             # This file
```

## Key Components

### Task constants (`tasks.go`)

String constants used as the asynq task type. The same constants are used in `cmd/worker/main.go` for both `mux.HandleFunc` registration and `scheduler.Register` cron scheduling.

| Constant | Value | Schedule |
|---|---|---|
| `TaskOpenRouterScrape` | `"scrape:openrouter"` | Every 6 hours |
| `TaskLiteLLMScrape` | `"scrape:litellm"` | Every 24 hours |
| `TaskHuggingFaceScrape` | `"scrape:huggingface"` | Every 24 hours |
| `TypeWebhookDeliver` | `"webhook:deliver"` | On-demand (enqueued by reconciler) |

### WorkerStore (`store.go`)

```go
type WorkerStore interface {
    FetchModels(ctx context.Context) ([]models.Model, error)
    FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error)
    EnsureModels(ctx context.Context, models []scraper.ScrapedModel) error
    MarkVerified(ctx context.Context, sourceName string, slugs []string) error
}
```

`FetchModels` returns all rows from the `models` table (used by the diff engine to resolve model IDs to slugs). `FetchPricesBySource` returns all `prices` rows for the named source via `JOIN sources` — the result is the baseline the diff engine compares incoming scraped data against. `EnsureModels` upserts any model slugs from incoming scraped data that do not yet exist in the `models` table, creating them before the diff engine runs. `MarkVerified` bumps `prices.last_verified_at = NOW()` for every price row of the named source whose model slug was reported this cycle — the freshness signal the API reads, recorded even when a price did not change (and therefore never reached the reconciler).

`NewPgxStore(db *pgxpool.Pool) WorkerStore` returns the production implementation.

### Handlers (`handlers.go`)

`NewHandlers(store WorkerStore, rec *reconciler.Reconciler, db *pgxpool.Pool) *Handlers` is the constructor. Public methods (`HandleOpenRouterScrape`, `HandleLiteLLMScrape`, `HandleHuggingFaceScrape`) delegate to the private `runPipeline` helper.

**Pipeline per handler:**
1. `slog.Info("handler: starting", ...)` — structured log with task name
2. Instantiate scraper with default HTTP client (`nil`)
3. `scraper.Fetch(ctx)` → `[]scraper.ScrapedModel`
4. `store.FetchModels(ctx)` → stored model metadata
5. `store.FetchPricesBySource(ctx, sourceName)` → stored prices for this source
6. `diff.Diff(storedPrices, storedModels, scraped)` → `[]diff.PriceDiff`
7. `reconciler.Reconcile(ctx, diffs)` — mediates all writes to `price_history`
8. `store.MarkVerified(ctx, sourceName, slugs)` — stamps `last_verified_at` for every scraped model so freshness reflects re-verification, not just the last change (best-effort: a failure here is logged but does not fail the scrape)
9. Sets `llm_source_last_success_timestamp_seconds{source}` to now — an eager freshness anchor; the ticker-driven sampler remains the authority
10. `slog.Info("handler: done", ..., "model_count", ...)` — structured log
11. Return any error (signals asynq to retry the task)

Scrapers never write to the database directly; all writes go through the reconciler.

### Freshness sampler (`freshness.go`)

```go
type FreshnessSampler struct { /* unexported: querier, staleAfter */ }
func NewFreshnessSampler(db *pgxpool.Pool, staleAfter time.Duration) *FreshnessSampler
func (s *FreshnessSampler) Sample(ctx context.Context) error
```

`Sample` runs a **single** grouped query over `prices JOIN sources` and republishes three gauges per source that has published prices:

| Gauge | Meaning |
|---|---|
| `llm_source_last_success_timestamp_seconds{source}` | Unix timestamp of the source's most recent verification |
| `llm_prices_stale_ratio{source}` | Fraction (0–1) of the source's published prices older than `staleAfter` |
| `llm_prices_published{source}` | Published price count, so the ratio has absolute context |

The staleness threshold is bound as a query parameter (`$1::interval`) — never string-formatted — and compared against `COALESCE(last_verified_at, confirmed_at)`, the same fallback the API read path uses, so legacy rows with a NULL `last_verified_at` are measured from `confirmed_at` rather than silently counting as fresh. `DefaultStaleAfter` is 24h, matching the product promise and the medium-confidence window in `internal/api.ComputeTrustMeta`.

`Sample` returns the wrapped query error and publishes nothing on failure. Sources with no published prices are **skipped, not zeroed**: a zeroed ratio would read as "perfectly fresh" and a zeroed timestamp as "verified in 1970".

The sampler holds a narrow local `freshnessQuerier` interface rather than the concrete `*pgxpool.Pool` (and rather than a new `WorkerStore` method), so it is unit-testable with a mock and the existing store mocks are unaffected. `cmd/worker/main.go` runs it on a **60-second ticker with a 10-second per-sample timeout**, independent of the scrape pipeline — a sampler invoked only inside the pipeline would leave the gauges frozen at their last good values when a scraper stopped running, so `time() - last success` would stay small and the staleness alert would never fire.

### Webhook delivery (`webhook_handler.go`)

`HandleWebhookDeliver(ctx, task)` processes `webhook:deliver` asynq tasks:

1. Unmarshal `WebhookTaskPayload` from the task body.
2. Serialise the nested `WebhookPayload` event as JSON.
3. Compute `HMAC-SHA256(secret, eventJSON)` and add `X-LLMPricing-Signature: sha256=<hex>` header.
4. POST the event JSON to the registered URL with a 15-second timeout.
5. Return an error for non-2xx responses so asynq retries (max 3 retries, 30s task timeout).

`NewWebhookDeliverTask(payload WebhookTaskPayload) (*asynq.Task, error)` creates the enqueue-ready task.

**Types:**

```go
type WebhookPayload struct {
    ModelID        int       `json:"model_id"`
    Provider       string    `json:"provider"`
    OldPriceInput  float64   `json:"old_price_input"`
    OldPriceOutput float64   `json:"old_price_output"`
    NewPriceInput  float64   `json:"new_price_input"`
    NewPriceOutput float64   `json:"new_price_output"`
    ConfirmedAt    time.Time `json:"confirmed_at"`
    Source         string    `json:"source"`
}

type WebhookTaskPayload struct {
    WebhookID string         `json:"webhook_id"`
    URL       string         `json:"url"`
    Secret    string         `json:"secret"` // plaintext; enqueued by reconciler
    Event     WebhookPayload `json:"event"`
}
```

## Dependencies

| Package | Role |
|---|---|
| `internal/scraper` | `Scraper` interface accepted by `runPipeline` |
| `internal/scraper/openrouter` | OpenRouter API scraper |
| `internal/scraper/litellm` | LiteLLM GitHub JSON scraper |
| `internal/scraper/huggingface` | HuggingFace Inference Providers scraper |
| `internal/diff` | Diff engine — computes price changes |
| `internal/reconciler` | Reconciliation engine — mediates all DB writes |
| `internal/metrics` | Freshness gauges published by `FreshnessSampler` (and pipeline counters) |
| `internal/models` | Shared domain types (`Model`, `Price`) |
| `github.com/hibiken/asynq` | Task queue framework |
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pool |

## Usage

This package is not used directly. `cmd/worker/main.go` wires it:

```go
store := worker.NewPgxStore(db)
rec   := reconciler.New(db)
h     := worker.NewHandlers(store, rec)

mux.HandleFunc(worker.TaskOpenRouterScrape,  h.HandleOpenRouterScrape)
mux.HandleFunc(worker.TaskLiteLLMScrape,     h.HandleLiteLLMScrape)
mux.HandleFunc(worker.TaskHuggingFaceScrape, h.HandleHuggingFaceScrape)
mux.HandleFunc(worker.TypeWebhookDeliver,    worker.HandleWebhookDeliver)

scheduler.Register("@every 6h",  asynq.NewTask(worker.TaskOpenRouterScrape, nil))
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil))
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskHuggingFaceScrape, nil))
```

## Testing

```bash
go test ./internal/worker/...
```

`handlers_test.go` uses a `mockStore` implementing `WorkerStore` and a `mockScraper` implementing `scraper.Scraper`. The reconciler is backed by a `mockReconcilerStore` (via `reconciler.NewWithStore`) so tests run without a database.

`freshness_test.go` drives `FreshnessSampler` through a `mockFreshnessQuerier` covering healthy rows, all-stale rows, partially-stale rows, an empty result, a zero-count source (skipped, not zeroed), and a query error (nothing published). Gauge values are read by gathering the default registry, which can assert a source is *absent* — a per-child read cannot distinguish "never set" from "set to 0".

`webhook_handler_test.go` spins up an `httptest.Server` to verify HMAC signature correctness and that non-2xx responses return an error.
