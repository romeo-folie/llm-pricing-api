# internal/models

Domain model definitions.

## Purpose

Contains Go struct definitions for the core domain entities: models, prices, sources, price history, and review queue items. These structs are used across the repository layer, API handlers, and reconciliation engine.

## Structure

```
internal/models/
  README.md    # This file
```

Currently a placeholder — structs will be added in Phase 1 as the data pipeline and API layers are built.

## Dependencies

Standard library only (expected: `time`, `github.com/google/uuid` or similar).

## Planned Types

| Type | Corresponds To | Description |
|---|---|---|
| `Source` | `sources` table | Data source (OpenRouter, LiteLLM, provider docs) |
| `Model` | `models` table | LLM model with provider, modality, context window |
| `Price` | `prices` table | Current confirmed price for a model from a source |
| `PriceHistory` | `price_history` hypertable | Immutable historical price record |
| `ReviewQueueItem` | `review_queue` table | Flagged price discrepancy awaiting resolution |
