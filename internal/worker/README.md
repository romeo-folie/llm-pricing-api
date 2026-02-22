# internal/worker

Asynq task constants, DB read layer, handler functions, and webhook delivery job for the LLM Pricing Platform's background worker pipeline.

## Purpose

This package is the bridge between the asynq job queue and the scrape→diff→reconcile data pipeline. It defines:

- **Task name constants** used when registering handlers and scheduling cron jobs.
- **`WorkerStore`** — a DB read interface (and its pgx implementation) that supplies the diff engine with the current stored models and prices for each source.
- **`Handlers`** — one public handler method per data source, each executing the full pipeline: fetch scraped data, fetch stored data, compute diffs, reconcile.
- **Webhook delivery** — `HandleWebhookDeliver` processes `webhook:deliver` asynq tasks, signs the payload with HMAC-SHA256, and POSTs to the registered URL with retry on failure.

`cmd/worker/main.go` instantiates this package and wires it into the asynq server and cron scheduler.

## Structure

```
internal/worker/
  tasks.go              # String constants for asynq task names (including TypeWebhookDeliver, TypeBillingRevokeKey)
  store.go              # WorkerStore interface + pgxWorkerStore implementation
  handlers.go           # Handlers struct, runPipeline helper, scraper handler methods
  webhook_handler.go    # WebhookPayload, WebhookTaskPayload, NewWebhookDeliverTask, HandleWebhookDeliver
  billing_handler.go    # BillingRevokeKeyHandler, BillingRevokeKeyStore interface + pgx implementation
  handlers_test.go      # Unit tests using mock store and mock scraper
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
| `TypeBillingRevokeKey` | `"billing:revoke-key"` | On-demand, scheduled at `ends_at` (enqueued by LS webhook handler on `subscription_cancelled`) |

### WorkerStore (`store.go`)

```go
type WorkerStore interface {
    FetchModels(ctx context.Context) ([]models.Model, error)
    FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error)
    EnsureModels(ctx context.Context, models []scraper.ScrapedModel) error
}
```

`FetchModels` returns all rows from the `models` table (used by the diff engine to resolve model IDs to slugs). `FetchPricesBySource` returns all `prices` rows for the named source via `JOIN sources` — the result is the baseline the diff engine compares incoming scraped data against. `EnsureModels` upserts any model slugs from incoming scraped data that do not yet exist in the `models` table, creating them before the diff engine runs.

`NewPgxStore(db *pgxpool.Pool) WorkerStore` returns the production implementation.

### Handlers (`handlers.go`)

`NewHandlers(store WorkerStore, rec *reconciler.Reconciler) *Handlers` is the constructor. Public methods (`HandleOpenRouterScrape`, `HandleLiteLLMScrape`, `HandleHuggingFaceScrape`) delegate to the private `runPipeline` helper.

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

### Billing key revocation (`billing_handler.go`)

`BillingRevokeKeyHandler` processes `billing:revoke-key` asynq tasks. These are enqueued by the Lemon Squeezy webhook handler when a subscription is cancelled, scheduled to fire at the subscription's `ends_at` date.

**`Handle(ctx, task)` steps:**
1. Unmarshal `billingRevokeKeyPayload` (`unkey_key_id`, `ls_subscription_id`) from the task payload.
2. Validate `ls_subscription_id` is non-empty.
3. If `unkey_key_id` is missing from the payload, look it up from `billing_subscriptions` via `BillingRevokeKeyStore.GetSubscriptionKeyID`. A missing row wraps `asynq.SkipRetry` — retrying cannot fix a non-existent subscription.
4. Call `keys.RevokeKey(ctx, unkeyKeyID)` — permanently revokes access. If the key is already deleted (`*sdkerrors.ErrNotFound`), the revocation goal is met; the handler continues to step 5 rather than erroring, enabling idempotent retries after a partial failure.
5. Call `store.ExpireSubscription(ctx, lsSubID)` — sets `status = 'expired'` in the DB. Zero rows affected wraps `asynq.SkipRetry`.
6. Log the outcome.

**Retry behaviour:**
- `MaxRetry(3)` and `Timeout(30s)` are set at enqueue time in `handleCancelled`.
- `asynq.SkipRetry` is used for permanent failures (missing subscription, zero rows affected) to avoid wasting all three retries.
- `*sdkerrors.ErrNotFound` from `RevokeKey` is treated as a no-op, ensuring retries succeed even when a prior attempt revoked the key but failed to update the DB.

**`BillingRevokeKeyStore`** is a two-method interface abstracting the DB calls, keeping the handler testable without a live database:

```go
type BillingRevokeKeyStore interface {
    GetSubscriptionKeyID(ctx context.Context, lsSubID string) (string, error)
    ExpireSubscription(ctx context.Context, lsSubID string) error
}
```

`NewBillingRevokeKeyStore(db *pgxpool.Pool) BillingRevokeKeyStore` returns the production pgx implementation.

**Constructor**: `NewBillingRevokeKeyHandler(db *pgxpool.Pool, keys billing.KeyManager, log zerolog.Logger) *BillingRevokeKeyHandler` — creates the store internally from the pool so `cmd/worker/main.go` only needs to pass the raw pool.

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
| `internal/billing` | `KeyManager` interface used by `BillingRevokeKeyHandler` |
| `internal/scraper` | `Scraper` interface accepted by `runPipeline` |
| `internal/scraper/openrouter` | OpenRouter API scraper |
| `internal/scraper/litellm` | LiteLLM GitHub JSON scraper |
| `internal/scraper/huggingface` | HuggingFace Inference Providers scraper |
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

mux.HandleFunc(worker.TaskOpenRouterScrape,  h.HandleOpenRouterScrape)
mux.HandleFunc(worker.TaskLiteLLMScrape,     h.HandleLiteLLMScrape)
mux.HandleFunc(worker.TaskHuggingFaceScrape, h.HandleHuggingFaceScrape)
mux.HandleFunc(worker.TypeWebhookDeliver,    worker.HandleWebhookDeliver)

// Billing revoke-key handler (conditional on billing service availability)
billingRevokeHandler := worker.NewBillingRevokeKeyHandler(db, billingSvc.Keys, log)
mux.HandleFunc(worker.TypeBillingRevokeKey, billingRevokeHandler.Handle)

scheduler.Register("@every 6h",  asynq.NewTask(worker.TaskOpenRouterScrape, nil))
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil))
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskHuggingFaceScrape, nil))
```

## Testing

```bash
go test ./internal/worker/...
```

`handlers_test.go` uses a `mockStore` implementing `WorkerStore` and a `mockScraper` implementing `scraper.Scraper`. The reconciler is backed by a `mockReconcilerStore` (via `reconciler.NewWithStore`) so tests run without a database.

`webhook_handler_test.go` spins up an `httptest.Server` to verify HMAC signature correctness and that non-2xx responses return an error.

`billing_handler_test.go` uses mock implementations of `BillingRevokeKeyStore` and `billing.KeyManager`. It covers: happy paths (key in payload vs DB fallback), missing subscription ID, invalid JSON, non-ErrNotFound RevokeKey errors, the already-revoked idempotency path (both direct and `fmt.Errorf`-wrapped `*sdkerrors.ErrNotFound`), and DB update failures.
