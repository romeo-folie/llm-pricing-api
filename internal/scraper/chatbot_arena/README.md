# internal/scraper/chatbot_arena

Disabled Chatbot Arena benchmark scraper — no-op compatibility stub.

## Purpose

Chatbot Arena (LMSYS) has **no working public ingestion source**. The upstream endpoint `lmarena.ai/api/leaderboard` began returning `403` in 2025 and there is no public replacement.

This package keeps the `BenchmarkScraper` shape alive so the handler wiring compiles and the task type continues to exist, while doing nothing at runtime. `Scrape` logs an explicit warning and returns `nil` so a scheduled run cannot error-loop or trigger asynq retry storms.

**It is not registered on the cron scheduler.** The task handler is mounted (`worker.TaskChatbotArenaScrape`), so a manually enqueued task resolves cleanly, but nothing enqueues it on a schedule.

## Structure

```
internal/scraper/chatbot_arena/
  scraper.go       # No-op Scraper; logs a skip and returns nil
  scraper_test.go  # Asserts Scrape is a no-op and New returns non-nil
  README.md        # This file
```

## Key Components

### `Scraper`

```go
func New(db *pgxpool.Pool, client *http.Client) *Scraper
func (s *Scraper) SetLogger(l zerolog.Logger)
func (s *Scraper) Scrape(_ context.Context) error  // always nil
```

A compile-time assertion pins the contract:

```go
var _ scraper.BenchmarkScraper = (*Scraper)(nil)
```

`db` and `client` are retained purely for constructor-signature parity with the working benchmark scrapers (both carry `//nolint:unused`). Passing `nil` for the client still builds a properly configured one — 60-second timeout, SSRF-safe transport, redirect host checking — so re-enabling this scraper does not require re-deriving the safe HTTP setup.

## Usage

```go
// cmd/worker/main.go — handler registered, cron deliberately absent
mux.HandleFunc(worker.TaskChatbotArenaScrape, h.HandleChatbotArenaScrape)
```

Running it logs:

```
WARN chatbot_arena: skipping scrape — no public endpoint available
     benchmark="Chatbot Arena"
     reason="lmarena.ai/api/leaderboard returned 403 — API is no longer publicly accessible"
```

## Design Notes

- **Silence would be worse than a warning.** A stub that returned `nil` quietly would look like a healthy daily scrape in metrics and logs. The explicit warning keeps the gap visible.
- **Seed rows are not live data.** Historical Chatbot Arena rows may exist in `model_benchmark_scores` from migration `000016`. They remain readable but must not be presented as a live feed, and they age out through the normal 90-day staleness rule.
- **To re-enable:** implement `Scrape` against a real source, add the cron registration in `cmd/worker/main.go`, and drop the `//nolint:unused` markers. Model identity must go through [`slugmap.Resolve`](../slugmap/README.md) like the other benchmark scrapers.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/scraper` | `BenchmarkScraper` interface, `NewSSRFSafeTransport`, `CheckRedirectHost` |
| `github.com/jackc/pgx/v5/pgxpool` | Retained for constructor parity; unused |
| `github.com/rs/zerolog` | The skip warning |

Unlike the working benchmark scrapers, this package does **not** depend on `internal/intelligence` — it writes nothing.
