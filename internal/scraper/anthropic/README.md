# internal/scraper/anthropic

Anthropic Claude pricing documentation scraper.

## Purpose

Scrapes the public Anthropic pricing page and returns normalised `scraper.ScrapedModel` records for the pricing pipeline. Provider docs act as **ground truth** in reconciliation — the third source that lifts a change agreed by OpenRouter and LiteLLM to `high` confidence.

Runs daily as an asynq cron task (`TaskAnthropicScrape`). Emits records with `SourceName: "anthropic"`.

## Structure

```
internal/scraper/anthropic/
  scraper.go       # Scraper, New, Fetch, table parsing, slug canonicalisation
  scraper_test.go  # Parsing tests against embedded HTML fixtures
  README.md        # This file
```

## Key Components

### Source

| Constant | Value |
|---|---|
| `defaultURL` | `https://platform.claude.com/docs/en/about-claude/pricing` |

### `Scraper`

```go
func New(client *http.Client) *Scraper
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)
```

Implements the shared `scraper.Scraper` interface.

### `PricingTable`

Intermediate parsed-table representation (headers plus rows) built before conversion to `ScrapedModel`, so the parser can be exercised against fixtures with no HTTP.

### Parsing behaviour

| Concern | Test |
|---|---|
| Per-MTok → per-token conversion | `TestFetch_PriceConversion`, `TestParsePricePerMTok` |
| Marketing name → clean model name | `TestCleanModelName` |
| Clean name → canonical `anthropic/...` slug | `TestCanonicalAnthropicSlug` |
| `(deprecated)` and similar suffixes stripped | `TestFetch_DeprecatedSuffix` |
| Non-2xx upstream response | `TestFetch_NonOKStatus` |
| Empty or unparseable page | `TestFetch_EmptyHTML` |
| Context cancellation aborts the fetch | `TestFetch_ContextCancellation` |

Anthropic quotes prices per **MTok** (million tokens); the database stores per-token, so conversion happens here.

### Slug canonicalisation

Claude model families are the most collision-prone in the dataset — `claude-3-5-sonnet` and `claude-3-sonnet` are different models one character apart. `canonicalAnthropicSlug` maps a cleaned display name to a canonical slug, and `TestCanonicalAnthropicSlug` guards it. This is separate from [`slugmap`](../slugmap/README.md), which handles *benchmark leaderboard* names rather than pricing-page names.

## Usage

```go
s := anthropic.New(&http.Client{
    Timeout:   30 * time.Second,
    Transport: scraper.NewSSRFSafeTransport(),
})

models, err := s.Fetch(ctx)
if err != nil {
    return fmt.Errorf("anthropic scrape: %w", err)
}
// models flow into diff.Diff, then the reconciler — never a direct DB write
```

## Design Notes

- **A layout change yields zero rows, not wrong prices.** `TestFetch_EmptyHTML` pins this. The diff engine ignores models missing from an incoming batch, so a parser break cannot zero out stored prices.
- **Deprecated models are still reported** with the suffix stripped from the name — the reconciler decides what to do with them rather than the scraper dropping data.
- **Untrusted external input**: prices must be finite and non-negative; `NaN`, `Inf`, and negatives are rejected before storage.
- **Never writes to the database.** Persistence is the reconciler's exclusive responsibility.
- **Caller supplies timeout and SSRF-safe transport.**

## Dependencies

| Dependency | Role |
|---|---|
| `internal/scraper` | `Scraper` interface, `ScrapedModel`, SSRF-safe transport |
| `golang.org/x/net/html` | HTML tokenisation and tree walking |

Consumed by `internal/worker` (`HandleAnthropicScrape`).
