# internal/review

Operator review queue for flagged price discrepancies. Provides an HTTP handler
and database store for presenting pending `review_queue` entries to human
operators and recording their approve/reject decisions.

---

## Purpose

When the reconciliation engine detects a price disagreement between two sources
that exceeds the 5% threshold, it inserts a row into `review_queue` with
`status = 'pending'`. This package exposes:

- A plain-HTML web UI at `GET /admin/review` listing all pending entries.
- A `POST /admin/review/:id/approve` endpoint that publishes the source-A value
  as a confirmed price and marks the entry as `'resolved'`.
- A `POST /admin/review/:id/reject` endpoint that discards the entry by marking
  it as `'overridden'` without writing any price data.

All actions are idempotent with respect to the database: repeated approvals are
suppressed by `ON CONFLICT DO NOTHING` on `price_history`, and the status column
prevents double-actioning.

---

## Structure

| Path | Role |
| --- | --- |
| `store.go` | `Store` interface and `pgxReviewStore` PostgreSQL implementation |
| `handler.go` | Fiber HTTP handler (List, Approve, Reject) and embedded template |
| `templates/review.html` | Server-rendered HTML template for the operator UI |
| `review_test.go` | Unit tests using a mock store and Fiber test utilities |
| `README.md` | This file |

---

## Key Components

### ReviewItem

`ReviewItem` is a denormalised view of a `review_queue` row that includes the
model name, provider, and source names resolved via JOIN. The handler passes a
`[]ReviewItem` slice directly to the HTML template.

```go
type ReviewItem struct {
    ID          int
    ModelID     int
    ModelName   string
    Provider    string
    Field       models.PriceField  // "input_cost_per_token" | "output_cost_per_token"
    SourceAID   int
    SourceAName string
    SourceBID   int
    SourceBName string
    ValueA      float64
    ValueB      float64
    DeltaPct    float64            // fractional, e.g. 0.12 = 12%
    Status      models.ReviewStatus
    CreatedAt   time.Time
}
```

### Store interface

```go
type Store interface {
    ListPending(ctx context.Context) ([]ReviewItem, error)
    Approve(ctx context.Context, id int) error
    Reject(ctx context.Context, id int) error
}
```

`NewPgxStore(db *pgxpool.Pool) Store` returns the production PostgreSQL
implementation.

#### Approve transaction

`Approve` wraps six steps in a single `pgx` transaction:

1. Fetch the `review_queue` row (model_id, source_a, value_a, field).
2. Fetch the current price for (model_id, source_a) — defaults to 0 if missing.
3. Determine input/output values from the field name and value_a.
4. `INSERT INTO price_history … ON CONFLICT DO NOTHING` (immutable history).
5. `INSERT INTO prices … ON CONFLICT (model_id, source_id) DO UPDATE SET …` (current price upsert).
6. `UPDATE review_queue SET status='resolved', resolved_at=NOW()`.

A deferred `Rollback` guarantees cleanup on any step failure.

### Handler

`Handler` wraps a `Store` and an `*html/template.Template` parsed once at
construction from the embedded `templates/review.html` file. Methods are
registered as Fiber route handlers.

```go
h := review.NewHandler(store)
app.Get("/admin/review",            h.List)
app.Post("/admin/review/:id/approve", h.Approve)
app.Post("/admin/review/:id/reject",  h.Reject)
```

The template is embedded at compile time via `//go:embed templates/review.html`,
so no filesystem access is required at runtime.

---

## Dependencies

| Dependency | Role |
| --- | --- |
| `internal/models` | `PriceField` and `ReviewStatus` typed constants |
| `github.com/jackc/pgx/v5/pgxpool` | Connection pool for database queries |
| `github.com/gofiber/fiber/v2` | HTTP routing and response helpers |
| `html/template` + `embed` | Server-side HTML rendering |
| `log/slog` | Structured logging for errors and warnings |

---

## Usage

### Wiring in cmd/api/main.go

```go
import "llm-pricing-api/internal/review"

// Construct the store backed by the shared DB pool.
reviewStore := review.NewPgxStore(db)

// Construct the handler.
reviewHandler := review.NewHandler(reviewStore)

// Register routes on the Fiber app (authenticated/admin-only group recommended).
app.Get("/admin/review",               reviewHandler.List)
app.Post("/admin/review/:id/approve",  reviewHandler.Approve)
app.Post("/admin/review/:id/reject",   reviewHandler.Reject)
```

### Testing with a mock store

Implement the `Store` interface with an in-memory mock and pass it to
`review.NewHandler(mockStore)`. The test file (`review_test.go`) demonstrates
this pattern using `fiber.App.Test` with `httptest.NewRequest`.
