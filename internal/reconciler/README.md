# internal/reconciler

Price reconciliation engine — the critical data-integrity boundary of the system.

## Purpose

Mediates **all writes** to `price_history` and `prices`. No scraper may write prices directly;
every confirmed change flows through the `Reconciler`. It enforces:

- **2-source agreement**: a price is published only when ≥2 sources agree (or a single source
  reports the same value in 2 consecutive scraper cycles).
- **Discrepancy flagging**: any source pair that disagrees by >5% is written to `review_queue`
  for human review instead of being published.
- **Majority-vote consensus**: when 2 of N sources agree exactly and a third outlier disagrees
  by >5%, the reconciler flags the outlier **and** publishes the 2-source consensus — a single
  noisy source does not block the majority.
- **Immutable history**: every confirmed change becomes an append-only row in `price_history`
  (TimescaleDB hypertable).

## Structure

```
internal/reconciler/
  reconciler.go       # Reconciler type, Reconcile method, multi- and single-source logic, webhook fan-out
  store.go            # Store interface + pgxStore (PostgreSQL-backed implementation)
  reconciler_test.go  # Unit tests using an in-memory mockStore
  README.md           # This file
```

## Key Components

### `Store` interface (`store.go`)

Database abstraction that allows the `Reconciler` to be tested without a real database.
Implementations must be safe for concurrent use.

| Method | Purpose |
|--------|---------|
| `LookupSourceID(ctx, name)` | Resolve a source name to its DB primary key |
| `LookupSourceName(ctx, id)` | Resolve a source primary key to its name (used for webhook payloads) |
| `LookupModelID(ctx, slug)` | Resolve a model slug to its DB primary key |
| `LookupModelProvider(ctx, id)` | Resolve a model primary key to its provider name (used for webhook payloads) |
| `LookupCurrentPrice(ctx, modelID, sourceID)` | Fetch current input/output costs for the unchanged field |
| `PublishPrice(ctx, modelID, sourceID, input, output, confidence)` | Transactionally insert into `price_history` and upsert `prices` |
| `FlagDiscrepancy(ctx, modelID, srcA, srcB, field, valA, valB, delta)` | Insert into `review_queue` (idempotent via `ON CONFLICT DO NOTHING`) |
| `ListActiveWebhooks(ctx)` | Return all non-deleted `webhooks` rows (used by the webhook fan-out after publish) |

`pgxStore` is the production PostgreSQL implementation backed by `*pgxpool.Pool`.

### `WebhookRow` type (`store.go`)

```go
type WebhookRow struct {
    ID         string
    APIKeyHash string
    URL        string
    Secret     string // plaintext (stored as-is in DB so the delivery worker can sign)
}
```

### `Reconciler` struct (`reconciler.go`)

Holds a `Store` reference, an in-memory `pending` map (key: `slug+":"+field+":"+source`)
that persists across `Reconcile` calls, and an optional `*asynq.Client` for webhook delivery.

**Constructors:**
- `New(db *pgxpool.Pool) *Reconciler` — production use
- `NewWithStore(s Store) *Reconciler` — test use (pass any `Store` implementation)

**Setter:**
```go
func (r *Reconciler) SetAsynqClient(c *asynq.Client)
```
Attaches an asynq client to enable webhook fan-out after every confirmed price publish. Passing `nil` (the default) disables webhook delivery.

**Main method:**
```go
func (r *Reconciler) Reconcile(ctx context.Context, diffs []diff.PriceDiff) error
```

Decision logic per (slug, field) group:

| Scenario | Action |
|----------|--------|
| 1 source, 1st cycle | Held in pending map (not published) |
| 1 source, 2nd cycle (same value) | Published with `ConfidenceMedium` |
| 1 source, value changed | Counter reset; stays pending |
| 2+ sources, all agree within ε | Published with `ConfidenceHigh` |
| 2+ sources, consensus ≥2, outlier >5% | **Flag AND publish** `ConfidenceHigh` |
| 2+ sources, all disagree, max delta >5% | Flagged for review; not published |
| 2+ sources, minor spread (≤5%) | Published with `ConfidenceMedium` |

**Pending map TTL:** entries not refreshed within 72 hours are swept by `sweepStalePending()`
at the start of each `Reconcile` call, bounding memory growth.

> **Known limitation:** the pending map is in-memory only. Process restarts (e.g., deploys)
> will reset it, causing single-source changes to re-enter as "first occurrence". Redis
> persistence for the pending map is deferred to the worker-wiring phase.

### `pendingChange` struct

```go
type pendingChange struct {
    value      float64
    source     string
    fetchCount int
    lastSeen   time.Time // updated each cycle; entries older than 72h are swept
}
```

## Dependencies

| Dependency | Role |
|---|---|
| `github.com/hibiken/asynq` | Task queue client used for webhook fan-out (optional) |
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pool (production `pgxStore`) |
| `github.com/jackc/pgx/v5` | `pgx.ErrNoRows` check in `LookupCurrentPrice` |
| `llm-pricing-api/internal/diff` | `PriceDiff` type consumed by `Reconcile` |
| `llm-pricing-api/internal/models` | `PriceField`, `Confidence` constants |

## Usage

```go
// Production: pass the pgxpool.Pool from internal/database
r := reconciler.New(db)

// Optionally attach an asynq client to enable webhook delivery.
// The client is safe to set before the first Reconcile call.
asynqClient := asynq.NewClient(redisOpt)
r.SetAsynqClient(asynqClient)

err := r.Reconcile(ctx, diffs)

// Testing: pass a mockStore implementing the Store interface
r := reconciler.NewWithStore(myMockStore)
// No asynq client → webhook fan-out is disabled in tests
```

## Testing Notes

Unit tests (`reconciler_test.go`) use an in-memory `mockStore` and cover all reconciliation
branches, error injection paths, concurrent access, and edge cases such as zero-price inputs
and 3-source majority votes. Run with:

```bash
go test -race ./internal/reconciler/...
```

The `pgxStore` (production DB layer) is not covered by unit tests — it requires a live
PostgreSQL + TimescaleDB instance and is exercised in the integration test harness (Issue #10).
