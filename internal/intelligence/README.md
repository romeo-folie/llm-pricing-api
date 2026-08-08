# intelligence

Stores and aggregates model benchmark scores into per-dimension capability scores.

## Purpose

This package is the data layer for the "Use-Case Intelligence" feature. It manages:

1. **Benchmark scores** — raw and normalised evidence from public benchmarks, stored as history with nullable upstream model and entry identities.
2. **Capability scores** — aggregated per-dimension scores (quality, coding, reasoning, …) derived from benchmark data via a weighted-average formula.

It sits between the scraper/ingestion layer (which feeds benchmark data in) and the API/recommendation layer (which reads capability scores out).

## Structure

```
internal/intelligence/
├── README.md              # This file
├── benchmark_store.go     # CRUD for model_benchmark_scores table
├── capability_store.go    # CRUD for model_capability_scores table
├── aggregator.go          # Weighted-average computation + DB orchestration
└── aggregator_test.go     # Table-driven unit tests for the Aggregate function
```

## Key Components

### Types

- **`BenchmarkScore`** — mirrors a `model_benchmark_scores` row, including source URL, exact upstream model/entry names, confidence, immutable evaluation date, and latest source-observation time.
- **`CapabilityScore`** — mirrors a `model_capability_scores` row. Fields: model ID, dimension, aggregated score, confidence, benchmark count, freshness.
- **`AggregatedResult`** — intermediate output of the pure `Aggregate` function before DB upsert.

### Store Functions

- `UpsertBenchmarkScore` — idempotently inserts immutable, content-versioned benchmark history; identical retries update only `last_observed_at`.
- `GetActiveBenchmarkScores` — reads one row per benchmark using source observation time, evaluation time, then stable evidence-content tie-breakers. Benchmark version strings are not parsed as recency signals.
- `UpsertCapabilityScore` / `GetCapabilityScores` — write and read capability scores by model.

### Aggregation

- `Aggregate(dimension, weights, scores, now)` — computes a weighted average, caps confidence by the contributing evidence, and marks the dimension stale when its oldest contributing score exceeds 90 days.
- `ComputeCapabilityScores(ctx, db, modelID)` — transactionally replaces a model's supported capability dimensions and deletes obsolete dimensions.
- `ComputeAllCapabilityScores(ctx, db)` — recomputes models appearing in either raw evidence or derived capability rows, so models that lose all evidence are cleaned up.

### Configuration

- `DimensionBenchmarks` — exported map defining which benchmarks contribute to each dimension and their relative weights.
- `StalenessThresholdDays` — 90-day threshold for marking scores as stale.

## Dependencies

- `github.com/jackc/pgx/v5/pgxpool` — PostgreSQL connection pool
- `github.com/google/uuid` — UUID handling for benchmark IDs

## Usage

```go
// Upsert a benchmark score from an ingestion pipeline.
err := intelligence.UpsertBenchmarkScore(ctx, db, intelligence.BenchmarkScore{
    ModelID:          42,
    BenchmarkID:      benchUUID,
    RawScore:         &raw,
    NormalizedScore:  &norm,
    BenchmarkVersion: "2024",
    SourceURL:        "https://example.com/leaderboard",
    SourceModelName:  &upstreamModel,
    SourceEntryName:  &upstreamEntry,
    Confidence:       "high",
    EvaluatedAt:      time.Now(),
})

// Recompute capability scores for a single model.
err = intelligence.ComputeCapabilityScores(ctx, db, 42)

// Recompute all models (e.g. from a cron job).
err = intelligence.ComputeAllCapabilityScores(ctx, db)
```
