# intelligence

Stores and aggregates model benchmark scores into per-dimension capability scores.

## Purpose

This package is the data layer for the "Use-Case Intelligence" feature. It manages:

1. **Benchmark scores** — raw and normalised results from public LLM benchmarks (MMLU-Pro, SWE-bench, Chatbot Arena, etc.), stored per model.
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

- **`BenchmarkScore`** — mirrors a `model_benchmark_scores` row. Fields: model ID, benchmark ID, raw/normalised score, confidence, source URL, evaluation date.
- **`CapabilityScore`** — mirrors a `model_capability_scores` row. Fields: model ID, dimension, aggregated score, confidence, benchmark count, freshness.
- **`AggregatedResult`** — intermediate output of the pure `Aggregate` function before DB upsert.

### Store Functions

- `UpsertBenchmarkScore` / `GetBenchmarkScores` — write and read benchmark scores by model.
- `UpsertCapabilityScore` / `GetCapabilityScores` — write and read capability scores by model.

### Aggregation

- `Aggregate(dimension, weights, scores, now)` — pure function that computes a weighted average for one dimension. Determines confidence (low/medium/high based on benchmark coverage) and freshness (stale if oldest score > 90 days).
- `ComputeCapabilityScores(ctx, db, modelID)` — orchestrates: fetch benchmark scores → resolve benchmark names → aggregate per dimension → upsert results.
- `ComputeAllCapabilityScores(ctx, db)` — iterates all models with benchmark data and recomputes.

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
    Confidence:       "high",
    EvaluatedAt:      time.Now(),
})

// Recompute capability scores for a single model.
err = intelligence.ComputeCapabilityScores(ctx, db, 42)

// Recompute all models (e.g. from a cron job).
err = intelligence.ComputeAllCapabilityScores(ctx, db)
```
