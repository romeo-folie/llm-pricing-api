# internal/scraper/livecodebench

LiveCodeBench benchmark scraper.

> Formerly named `huggingface_llm`, which wrongly implied a HuggingFace source. Not to be confused
> with [`internal/scraper/huggingface`](../huggingface/README.md), which really is a *pricing* scraper.
>
> The asynq task type is `benchmark:livecodebench`; the pre-rename `benchmark:huggingface_llm` value
> stays registered as a deprecated alias so queued jobs drain (see `internal/worker/tasks.go`).

## Purpose

Fetches per-question pass@1 results from the LiveCodeBench performance dataset, aggregates them into a per-model mean, and writes the result as benchmark evidence via [`internal/intelligence`](../../intelligence/README.md). Runs daily as an asynq cron task.

Supplies the `coding` capability dimension.

## Structure

```
internal/scraper/livecodebench/
  scraper.go            # Scraper type, fetch, pass@1 aggregation, evidence upsert
  scraper_test.go       # Parsing, averaging, percentage normalisation, order-independence
  slug_metrics_test.go  # Per-entry slug-resolution outcome counter tests
  README.md             # This file
```

## Key Components

### Sources

| Constant | Value |
|---|---|
| `dataURL` | `https://raw.githubusercontent.com/LiveCodeBench/livecodebench.github.io/main/src/mocks/performances_generation.json` |
| `sourceURL` | `https://livecodebench.github.io/leaderboard.html` |
| `benchmarkName` | `LiveCodeBench` |

### `Scraper`

```go
func New(db *pgxpool.Pool, client *http.Client) *Scraper
func (s *Scraper) SetLogger(l zerolog.Logger)
func (s *Scraper) Scrape(ctx context.Context) error
```

Implements the shared `scraper.BenchmarkScraper` interface.

### Slug resolution observability

Each entry is resolved through the unexported `resolveSlug` helper, which tries `model_name` and then
`model_repr` and increments `llm_slug_resolutions_total{result}` **once per entry**
(`resolved` / `unknown` / `ambiguous`). Counting lookups instead of entries would double-count every
fallback failure and inflate the unknown rate that `LLMSlugResolutionUnknownRateHigh` keys on.
Ambiguous names still resolve to the greedy first match, so adding the counter cannot shrink
coverage. The helper is unit-tested in `slug_metrics_test.go` without a database.

### pass@1 aggregation

The upstream payload reports results per question, not per model. The scraper averages them into a **mean pass@1 percentage (0–100)** per model. Two normalisation details are covered by tests:

- Values already expressed as percentages are not multiplied again (`TestPass1_AlreadyPercentage`).
- Aggregation is order-independent, so a reordered upstream file yields byte-identical evidence (`TestAggregation_AveragePass1`, `TestSelectBestResolvedModels_OrderIndependent`).

### Content-addressed evidence versions

As with the SWE-bench scraper, the evidence version is a SHA-256 digest of score content and provenance rather than a timestamp — an unchanged re-scrape advances only `last_observed_at` (`TestEvidenceVersion_IsContentAddressed`).

## Usage

```go
// cmd/worker/main.go
mux.HandleFunc(worker.TaskLiveCodeBenchScrape, h.HandleLiveCodeBenchScrape)
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiveCodeBenchScrape, nil))
```

## Design Notes

- **Identity via allowlist.** Model names resolve through [`slugmap.ResolveOutcome`](../slugmap/README.md); names with no match are logged and skipped, and every outcome is counted in `llm_slug_resolutions_total`. Ambiguous names are counted as ambiguous but still resolve to the greedy first match.
- **Multiple upstream names can map to one model**, so the scraper deduplicates deterministically before writing (`TestModelNamesByRepresentation_OrderIndependent`).
- **Failure propagates** so asynq retries rather than leaving benchmark state half-written.
- **Untrusted input**: external JSON is validated before use; the HTTP client needs an explicit timeout and the SSRF-safe transport.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/intelligence` | Benchmark evidence upsert and capability recompute |
| `internal/metrics` | `llm_slug_resolutions_total` outcome counter |
| `internal/scraper` | `BenchmarkScraper` interface, SSRF-safe transport |
| `internal/scraper/slugmap` | Leaderboard name → canonical DB slug |
| `github.com/jackc/pgx/v5/pgxpool` | Postgres access |
| `github.com/rs/zerolog` | Structured logging |
