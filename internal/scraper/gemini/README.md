# internal/scraper/gemini

Google Gemini API pricing documentation scraper.

## Purpose

Scrapes the public Gemini API pricing page and returns normalised `scraper.ScrapedModel` records for the pricing pipeline. Provider docs act as **ground truth** in reconciliation — the third source that lifts a change agreed by OpenRouter and LiteLLM to `high` confidence.

Runs daily as an asynq cron task (`TaskGeminiScrape`). Emits records with `SourceName: "google"`.

## Structure

```
internal/scraper/gemini/
  scraper.go       # Scraper, New, Fetch, parseHTML, extractPricing
  scraper_test.go  # Tier, emoji, multi-line and tiered-price fixtures
  README.md        # This file
```

## Key Components

### Source

| Constant | Value |
|---|---|
| `defaultURL` | `https://ai.google.dev/gemini-api/docs/pricing` |
| `userAgent` | `Googlebot/2.1 (+http://www.google.com/bot.html)` |

### `Scraper`

```go
func New(client *http.Client) *Scraper
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)
```

Implements the shared `scraper.Scraper` interface.

### Page shape and `ModelPricing`

The Gemini pricing page is structured as a repeating `H2 → H3 → TABLE` triplet:

- **H2** names the model.
- **H3** names the tier — `Standard` or `Batch`.
- **TABLE** carries the rows, where column 1 is Free Tier and **column 2 is Paid Tier**.

`parseHTML` walks that structure, tracking the current model and tier and resetting the tier on each new H2. `extractPricing` pulls the Input and Output paid-tier cells out of one table into a `ModelPricing` intermediate (`Model`, `Tier`, `InputRaw`, `OutputRaw`) before conversion.

### Extraction rules

| Rule | Test |
|---|---|
| Only **Standard**-tier tables are used; Batch tables are skipped | `TestFetch_BatchTierSkipped` |
| Per-million → per-token conversion | `TestFetch_PriceConversion`, `TestParsePricePerMillion` |
| Context-length-tiered prices handled | `TestFetch_TieredContextPrice` |
| Emoji stripped from model headings | `TestFetch_EmojiStripped` |
| Multi-line output-price cells parsed | `TestFetch_MultiLineOutputTextPrice` |
| Display name → clean name → slug | `TestCleanModelName`, `TestNormalizeSlug` |
| Non-2xx upstream response | `TestFetch_NonOKStatus` |
| Empty or unparseable page | `TestFetch_EmptyHTML` |
| Context cancellation aborts the fetch | `TestFetch_ContextCancellation` |

Models whose paid-tier prices are non-numeric (e.g. "Free of charge", "Not available") are skipped rather than coerced to zero — a free tier is not a $0 paid price.

## Usage

```go
s := gemini.New(&http.Client{
    Timeout:   30 * time.Second,
    Transport: scraper.NewSSRFSafeTransport(),
})

models, err := s.Fetch(ctx)
if err != nil {
    return fmt.Errorf("gemini scrape: %w", err)
}
// models flow into diff.Diff, then the reconciler — never a direct DB write
```

## Design Notes

- **Explicit User-Agent.** The page is served differently to unknown agents, so the scraper identifies as Googlebot. Changing this string is likely to change what gets parsed — treat it as part of the contract.
- **Free tier ≠ zero price.** Skipping non-numeric paid-tier values is deliberate; writing `0.0` would make free-tier models look like the cheapest paid option in `/v1/recommend`.
- **Batch tier is out of scope.** Batch pricing is a different product with different guarantees, so mixing it into the same model row would misreport the price a caller would actually pay.
- **A layout change yields zero rows, not wrong prices** (`TestFetch_EmptyHTML`). The diff engine ignores absent models, so a parser break cannot zero out stored prices.
- **Untrusted external input**: prices validated as finite and non-negative before storage.
- **Never writes to the database.** Persistence is the reconciler's exclusive responsibility.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/scraper` | `Scraper` interface, `ScrapedModel`, SSRF-safe transport |
| `golang.org/x/net/html` | HTML tokenisation and tree walking |

Consumed by `internal/worker` (`HandleGeminiScrape`).
