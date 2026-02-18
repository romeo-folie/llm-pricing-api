# internal/models

Domain model definitions for the LLM pricing platform.

## Purpose

Contains Go struct definitions for all core domain entities. These types are the shared vocabulary across the database layer, scraper pipeline, reconciliation engine, and API handlers. The package has no dependencies outside the standard library.

## Structure

```
internal/models/
  model.go    # Model and Source structs; Modality and SourceType constants
  price.go    # Price, PriceHistory, and ReviewQueueItem structs; Confidence, PriceField, ReviewStatus constants
  README.md   # This file
```

## Key Types

### model.go

| Type | DB Table | Description |
|------|----------|-------------|
| `Model` | `models` | An LLM model with provider, slug, modality, and context window |
| `Source` | `sources` | A data source (OpenRouter API, LiteLLM GitHub JSON, or provider HTML page) |
| `Modality` | — | String enum: `text`, `multimodal`, `image`, `audio`, `embedding` |
| `SourceType` | — | String enum: `api`, `github`, `scrape` |

### price.go

| Type | DB Table | Description |
|------|----------|-------------|
| `Price` | `prices` | Current confirmed price for a (model, source) pair; at most one row per pair |
| `PriceHistory` | `price_history` | Immutable record of every confirmed price change (TimescaleDB hypertable) |
| `ReviewQueueItem` | `review_queue` | A flagged discrepancy between sources awaiting operator approval |
| `Confidence` | — | String enum: `high`, `medium`, `low` |
| `PriceField` | — | String enum: `input_cost_per_token`, `output_cost_per_token` |
| `ReviewStatus` | — | String enum: `pending`, `resolved`, `overridden` |

## Design Notes

- **No circular imports**: scrapers and the reconciler both import this package; this package imports nothing from them.
- **`float64` for prices**: the DB uses `NUMERIC(20,10)`; `float64` is used in Go for Phase 1 to avoid an external decimal library. Precision loss is acceptable at this stage.
- **Nullable fields as pointers**: `Model.ContextWindow` and `ReviewQueueItem.ResolvedAt` are `*int` / `*time.Time` to represent SQL NULL.

## Dependencies

Standard library only (`time`).
