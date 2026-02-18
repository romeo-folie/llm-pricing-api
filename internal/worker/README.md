# internal/worker

Asynq task constants, DB read layer, and handler functions for the LLM Pricing Platform's background scraper pipeline.

## Purpose

This package is the bridge between the asynq job queue and the scrape→diff→reconcile data pipeline. It defines:

- **Task name constants** used when registering handlers and scheduling cron jobs.
- **`WorkerStore`** — a DB read interface (and its pgx implementation) that supplies the diff engine with the current stored models and prices for each source.
- **`Handlers`** — one public handler method per data source, each executing the full pipeline: fetch scraped data, fetch stored data, compute diffs, reconcile.

`cmd/worker/main.go` instantiates this package and wires it into the asynq server and cron scheduler.

## Structure

```
internal/worker/
  tasks.go        # String constants for all 7 asynq task names
  store.go        # WorkerStore interface + pgxWorkerStore implementation
  handlers.go     # Handlers struct, runPipeline helper, 7 public handler methods
  handlers_test.go # Unit tests using mock store and mock scraper
  README.md       # This file
```

## Key Components

### Task constants (`tasks.go`)

Seven `string` constants of the form `"scrape:<source>"` used as the asynq task type. The same constants are used in `cmd/worker/main.go` for both `mux.HandleFunc` registration and `scheduler.Register` cron scheduling.

| Constant | Value | Schedule |
|---|---|---|
| `TaskOpenRouterScrape` | `"scrape:openrouter"` | Every 6 hours |
| `TaskLiteLLMScrape` | `"scrape:litellm"` | Every 24 hours |
| `TaskOpenAIScrape` | `"scrape:openai"` | Every 24 hours |
| `TaskAnthropicScrape` | `"scrape:anthropic"` | Every 24 hours |
| `TaskGoogleScrape` | `"scrape:google"` | Every 24 hours |
| `TaskMistralScrape` | `"scrape:mistral"` | Every 24 hours |
| `TaskAmazonScrape` | `"scrape:amazon"` | Every 24 hours |

### WorkerStore (`store.go`)

```go
type WorkerStore interface {
    FetchModels(ctx context.Context) ([]models.Model, error)
    FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error)
}
```

`FetchModels` returns all rows from the `models` table (used by the diff engine to resolve model IDs to slugs). `FetchPricesBySource` returns all `prices` rows for the named source via `JOIN sources` — the result is the baseline the diff engine compares incoming scraped data against.

`NewPgxStore(db *pgxpool.Pool) WorkerStore` returns the production implementation.

### Handlers (`handlers.go`)

`NewHandlers(store WorkerStore, rec *reconciler.Reconciler) *Handlers` is the constructor. All seven public methods (`HandleOpenRouterScrape`, `HandleLiteLLMScrape`, `HandleOpenAIScrape`, `HandleAnthropicScrape`, `HandleGoogleScrape`, `HandleMistralScrape`, `HandleAmazonScrape`) delegate to the private `runPipeline` helper.

**Pipeline per handler:**
1. `slog.Info("handler: starting", ...)` — structured log with task name
2. Instantiate scraper with default HTTP client (`nil`)
3. `scraper.Fetch(ctx)` → `[]scraper.ScrapedModel`
4. `store.FetchModels(ctx)` → stored model metadata
5. `store.FetchPricesBySource(ctx, sourceName)` → stored prices for this source
6. `diff.Diff(storedPrices, storedModels, scraped)` → `[]diff.PriceDiff`
7. `reconciler.Reconcile(ctx, diffs)` — mediates all writes to `price_history`
8. `slog.Info("handler: done", ..., "model_count", ...)` — structured log
9. Return any error (signals asynq to retry the task)

Scrapers never write to the database directly; all writes go through the reconciler.

## Dependencies

| Package | Role |
|---|---|
| `internal/scraper` | `Scraper` interface accepted by `runPipeline` |
| `internal/scraper/openrouter` | OpenRouter API scraper |
| `internal/scraper/litellm` | LiteLLM GitHub JSON scraper |
| `internal/scraper/providers` | HTML scrapers for OpenAI, Anthropic, Google, Mistral, Amazon |
| `internal/diff` | Diff engine — computes price changes |
| `internal/reconciler` | Reconciliation engine — mediates all DB writes |
| `internal/models` | Shared domain types (`Model`, `Price`) |
| `github.com/hibiken/asynq` | Task queue framework |
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pool |

## Usage

This package is not used directly. `cmd/worker/main.go` wires it:

```go
store := worker.NewPgxStore(db)
rec   := reconciler.New(db)
h     := worker.NewHandlers(store, rec)

mux.HandleFunc(worker.TaskOpenRouterScrape, h.HandleOpenRouterScrape)
// ... remaining 6 handlers

scheduler.Register("@every 6h",  asynq.NewTask(worker.TaskOpenRouterScrape, nil))
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil))
// ... remaining 5 tasks
```

## Testing

```bash
go test ./internal/worker/...
```

The test file uses a `mockStore` implementing `WorkerStore` and a `mockScraper` implementing `scraper.Scraper`. The reconciler is backed by a `mockReconcilerStore` (via `reconciler.NewWithStore`) so tests run without a database.
