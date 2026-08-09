# internal/scraper/openai

OpenAI pricing documentation scraper.

## Purpose

Scrapes the public OpenAI API pricing page and returns normalised `scraper.ScrapedModel` records for the pricing pipeline. Provider docs act as **ground truth** in reconciliation: they are the third source that lets a change confirmed by OpenRouter and LiteLLM reach `high` confidence.

Runs daily as an asynq cron task (`TaskOpenAIScrape`). Emits records with `SourceName: "openai"`.

## Structure

```
internal/scraper/openai/
  scraper.go       # Scraper, New, Fetch, HTML table parsing and price normalisation
  scraper_test.go  # Table-driven parsing tests against embedded HTML fixtures
  README.md        # This file
```

## Key Components

### Source

| Constant | Value |
|---|---|
| `defaultURL` | `https://developers.openai.com/api/docs/pricing?latest-pricing=standard` |

### `Scraper`

```go
func New(client *http.Client) *Scraper
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)
```

Implements the shared `scraper.Scraper` interface. `Fetch` retrieves the page, walks the parsed HTML for pricing tables, and converts each row into a `ScrapedModel`.

### `PricingTable`

Intermediate representation of one parsed HTML table (headers plus rows) produced before conversion to `ScrapedModel`. Keeping this step separate is what makes the parser testable against fixtures without HTTP.

### Parsing behaviour

Covered directly by tests:

| Concern | Test |
|---|---|
| Per-million → per-token conversion | `TestFetch_PriceConversion`, `TestParsePricePerMillion` |
| Repeated model rows collapse to one record | `TestFetch_Deduplication` |
| Display name → canonical slug | `TestNormalizeSlug` |
| `colspan` in the final header row | `TestExtractHeaders_ColSpanInLastRow` |
| Non-2xx upstream response | `TestFetch_NonOKStatus` |
| Empty or unparseable page | `TestFetch_EmptyHTML` |
| Context cancellation aborts the fetch | `TestFetch_ContextCancellation` |

Published prices are per **million** tokens; the database stores per-token. Conversion happens here so nothing downstream has to know the page's units.

## Usage

```go
s := openai.New(&http.Client{
    Timeout:   30 * time.Second,
    Transport: scraper.NewSSRFSafeTransport(),
})

models, err := s.Fetch(ctx)
if err != nil {
    return fmt.Errorf("openai scrape: %w", err)
}
// models flow into diff.Diff, then the reconciler — never a direct DB write
```

## Design Notes

- **HTML scraping is inherently brittle.** A layout change surfaces as zero parsed rows rather than wrong prices; `TestFetch_EmptyHTML` pins that behaviour. Treat a sudden empty result as a parser break, not a price removal — the diff engine ignores models absent from an incoming batch, so stale prices are never silently zeroed.
- **Untrusted external input.** Prices are validated as finite, non-negative floats before storage; `NaN`, `Inf`, and negatives are rejected.
- **Never writes to the database.** `Fetch` is pure I/O-in, values-out. All persistence goes through the reconciler.
- **Timeouts and SSRF protection are the caller's job** — pass a client with an explicit timeout and `scraper.NewSSRFSafeTransport()`.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/scraper` | `Scraper` interface, `ScrapedModel`, SSRF-safe transport |
| `golang.org/x/net/html` | HTML tokenisation and tree walking |

Consumes nothing else; consumed by `internal/worker` (`HandleOpenAIScrape`).
