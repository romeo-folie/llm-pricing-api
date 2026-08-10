# cmd/tools/ingest_benchmark

CLI for manually inserting a single benchmark score.

## Purpose

Inserts one benchmark score for one model and recomputes that model's capability scores. It is the escape hatch for evidence the automated scrapers cannot reach: internal evaluations, private benchmarks, a leaderboard with no machine-readable feed, or a correction to a bad row.

Everything the daily scrapers do — evidence insertion, capability recompute — this does for a single hand-supplied data point, through the same [`internal/intelligence`](../../../internal/intelligence/README.md) entry points. It does not bypass the evidence model.

## Structure

```
cmd/tools/ingest_benchmark/
  main.go    # Flag parsing, validation, evidence insert, capability recompute
  README.md  # This file
```

## Key Components

### Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--model` | Yes | — | Model slug, e.g. `anthropic/claude-sonnet-4-6` |
| `--benchmark` | Yes | — | Benchmark name **exactly as stored in `benchmarks.name`** |
| `--score` | Yes | — | Raw score as a float |
| `--source` | Yes | — | Source URL, stored for attribution |
| `--confidence` | No | `medium` | `high`, `medium`, or `low` |
| `--evaluated` | No | today (UTC) | Evaluation date, `YYYY-MM-DD` |

### Validation

- `--model`, `--benchmark`, and `--source` must be non-empty.
- `--score` is required, but **an explicit `--score=0` is accepted**: the tool inspects `flag.Visit` to distinguish "not provided" from "provided as zero", so a genuine zero result can be recorded.
- `--confidence` must be one of the three allowed values.
- `--evaluated` must parse as `YYYY-MM-DD`; it defaults to today truncated to a day boundary, matching the day-granularity the scrapers use.
- A score outside 0–100 prints a warning to stderr but **does not abort** — not every benchmark is percentage-scaled.

### Configuration

Loads `.env` via `godotenv` if present, then requires `DATABASE_URL`. No other environment variable is needed — the tool talks only to Postgres.

## Usage

```bash
go run ./cmd/tools/ingest_benchmark \
  --model openai/gpt-4o \
  --benchmark "LiveCodeBench" \
  --score 91.3 \
  --source https://livecodebench.github.io/leaderboard.html
```

With every flag set:

```bash
go run ./cmd/tools/ingest_benchmark \
  --model=anthropic/claude-sonnet-4-6 \
  --benchmark="SWE-bench Verified" \
  --score=79.4 \
  --source=https://www.swebench.com/ \
  --confidence=high \
  --evaluated=2025-09-15
```

After inserting the score, the tool triggers a capability recompute for the affected model, so `/v1/recommend` and `/v1/compare` reflect the new evidence without waiting for the daily cron.

## Design Notes

- **`--benchmark` is matched against existing catalogue rows,** not created on demand. A typo fails rather than silently spawning a new benchmark; add genuinely new benchmarks via a migration.
- **Evidence stays immutable.** Re-running with identical content advances observation metadata rather than mutating history, consistent with the scraper ingestion path.
- **Confidence is the caller's assertion.** Unlike the SWE-bench scraper — which pins agent-system submissions to low base-model confidence automatically — nothing here infers confidence. Set `--confidence` honestly; `/v1/*` responses surface it to consumers.
- **Not a bulk loader.** One score per invocation, by design: a manual path with no batching is hard to use accidentally.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/intelligence` | Benchmark evidence insert and capability recompute |
| `github.com/jackc/pgx/v5/pgxpool` | Postgres connection pool |
| `github.com/joho/godotenv` | Optional `.env` loading |

Schema: `migrations/000013`–`000016` (benchmark catalogue, evidence, capability scores) and `000018` (provenance columns).
