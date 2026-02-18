# internal/diff

Pure-function price diff engine.

## Purpose

Computes the set of price changes between the currently stored prices and a fresh batch of scraped data from one source. Has no side effects — no database access, no HTTP, no logging. The output feeds directly into the reconciliation engine.

## Structure

```
internal/diff/
  diff.go       # Diff function and PriceDiff type
  diff_test.go  # 100% coverage unit tests
  README.md     # This file
```

## Key Types

### `PriceDiff`

```go
type PriceDiff struct {
    ModelSlug   string
    Field       models.PriceField  // "input_cost_per_token" or "output_cost_per_token"
    OldValue    float64
    NewValue    float64
    PctChange   float64  // signed; positive = price increase
    Source      string
    NeedsReview bool     // true when abs(PctChange) > 5%
}
```

### `Diff`

```go
func Diff(stored []models.Price, storedModels []models.Model, incoming []scraper.ScrapedModel) []PriceDiff
```

**Behaviour:**
- Matches incoming `ScrapedModel` to stored `Price` by model slug (resolved via `storedModels`).
- Emits one `PriceDiff` per changed field (`input_cost_per_token`, `output_cost_per_token`).
- Unchanged fields produce no diff.
- New models (slug not in stored) → two diffs with `OldValue = 0`, `NeedsReview = false`.
- Models in stored but absent from incoming → silently ignored (not treated as deletions).
- `NeedsReview = true` when `abs(PctChange) > 5%`.
- Division-by-zero safe: when `OldValue = 0`, `PctChange` is set to `1.0`.

## Usage

The worker handler is responsible for pre-filtering `stored` prices to the source matching `incoming`:

```go
// In the worker handler for the OpenRouter scraper:
scraped, _ := openrouterScraper.Fetch(ctx)
storedModels, _ := db.ListModels(ctx)
currentPrices, _ := db.ListPricesBySource(ctx, openrouterSourceID)

diffs := diff.Diff(currentPrices, storedModels, scraped)
reconciler.Reconcile(ctx, diffs)
```

## Design Notes

- **No DB access**: the caller fetches data; the diff engine only computes.
- **Epsilon comparison**: prices are `float64`; changes smaller than `1e-12` are treated as zero to avoid floating-point noise.
- **5% threshold**: controlled by the `reviewThreshold` constant — change in one place to affect the whole pipeline.

## Dependencies

| Dependency | Role |
|------------|------|
| `internal/models` | `Price`, `Model`, `PriceField` types |
| `internal/scraper` | `ScrapedModel` type |
| `math` (stdlib) | `math.Abs` for safe float comparison |
