# internal/scraper/swebench

SWE-bench Verified benchmark scraper.

## Purpose

Formerly named `bfcl`, which no longer described what it does. The asynq task type is
`benchmark:swebench`; the pre-rename `benchmark:bfcl` value stays registered as a deprecated alias
so queued jobs drain (see `internal/worker/tasks.go`).

Fetches resolved-percentage scores from the official SWE-bench leaderboard JSON and writes them as benchmark evidence via [`internal/intelligence`](../../intelligence/README.md). Runs daily as an asynq cron task; a successful run triggers a synchronous capability recompute.

Supplies the `coding` and `agentic` capability dimensions.

## Structure

```
internal/scraper/swebench/
  scraper.go       # Scraper type, Fetch/parse, best-entry selection, evidence upsert
  scraper_test.go  # Parsing, order-independence, tie-breaking, content-addressed version tests
  README.md        # This file
```

## Key Components

### Sources

| Constant | Value |
|---|---|
| `jsonURL` | `https://raw.githubusercontent.com/SWE-bench/swe-bench.github.io/master/data/leaderboards.json` |
| `sourceURL` | `https://www.swebench.com/` (human-readable, stored for attribution) |
| `benchmarkName` | `SWE-bench Verified` |

### `Scraper`

```go
func New(db *pgxpool.Pool, client *http.Client) *Scraper
func (s *Scraper) SetLogger(l zerolog.Logger)
func (s *Scraper) Scrape(ctx context.Context) error
```

Implements the shared `scraper.BenchmarkScraper` interface. `Scrape` fetches the leaderboard JSON, selects the Verified split, resolves each entry's model identity, picks the best submission per model, and upserts evidence.

### Best-entry selection

The leaderboard lists many agent-system submissions per base model. The scraper keeps the **highest resolved percentage** per canonical model, with deterministic tie-breaking by source model name so repeated runs produce identical results regardless of JSON ordering (`TestSelectBestEntries_OrderIndependent`).

Because a score belongs to an *agent system* rather than the base model alone, evidence is stored with **low base-model confidence** — the number describes what the model achieved inside a scaffold.

### Content-addressed evidence versions

The evidence version is a SHA-256 digest of the score content and provenance, not a timestamp. An unchanged re-scrape therefore advances only `last_observed_at` and preserves the immutable record; a changed score or provenance appends a new one (`TestEvidenceVersion_IsContentAddressed`).

## Usage

Registered as a worker task and cron entry:

```go
// cmd/worker/main.go
mux.HandleFunc(worker.TaskSWEBenchScrape, h.HandleSWEBenchScrape)
scheduler.Register("@every 24h", asynq.NewTask(worker.TaskSWEBenchScrape, nil))
```

Direct construction:

```go
s := swebench.New(db, &http.Client{
    Timeout:   30 * time.Second,
    Transport: scraper.NewSSRFSafeTransport(),
})
s.SetLogger(log)
if err := s.Scrape(ctx); err != nil { /* task fails and asynq retries */ }
```

## Design Notes

- **Unresolved models are skipped, never guessed.** Identity goes through [`slugmap.Resolve`](../slugmap/README.md); a miss is logged and dropped.
- **Failure propagates.** `Scrape` returning an error fails the asynq task so it is retried, rather than leaving a partially-updated benchmark table behind.
- **Scores are never written to pricing tables.** This scraper only touches benchmark evidence; capability scores are derived downstream by `intelligence.ComputeAllCapabilityScores`.
- **Untrusted input.** The leaderboard is external JSON; entries are schema- and type-checked before use, and the HTTP client must carry an explicit timeout and the SSRF-safe transport.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/intelligence` | Benchmark evidence upsert and capability recompute |
| `internal/scraper` | `BenchmarkScraper` interface, SSRF-safe transport |
| `internal/scraper/slugmap` | Leaderboard name → canonical DB slug |
| `github.com/jackc/pgx/v5/pgxpool` | Postgres access |
| `github.com/rs/zerolog` | Structured logging |
