# internal/scraper/openai

OpenAI pricing documentation scraper.

## Purpose

Scrapes the public OpenAI API pricing page and returns normalised `scraper.ScrapedModel` records for the pricing pipeline. Provider docs act as **ground truth** in reconciliation: they are the third source that lets a change confirmed by OpenRouter and LiteLLM reach `high` confidence.

Runs daily as an asynq cron task (`TaskOpenAIScrape`). Emits records with `SourceName: "openai"`.

## Structure

```
internal/scraper/openai/
  scraper.go       # Scraper, New, Fetch, Astro island props parsing, price normalisation
  scraper_test.go  # Parsing tests against embedded Astro island fixtures
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

Implements the shared `scraper.Scraper` interface. `Fetch` retrieves the page and extracts pricing from the **JSON props embedded on the page's Astro islands**, not from the visible tables.

### `pricingIsland`

Intermediate representation of one decoded `<astro-island>` pricing component plus the nearest section heading that preceded it. Produced by `parsePricingIslands`, consumed by `toScrapedModels`.

### `decodeAstroValue`

Decodes Astro's serialized prop format back into plain Go values. Every node is a two-element tuple `[type, value]`:

| Tuple | Meaning |
|---|---|
| `[0, primitive]` | string, number, boolean or null |
| `[0, {…}]` | object whose values are themselves encoded |
| `[1, […]]` | array of encoded values |

### Parsing behaviour

Covered directly by tests:

| Concern | Test |
|---|---|
| Full standard-tier text list, per-million → per-token conversion | `TestFetch`, `TestFetch_PriceConversion` |
| Batch/flex/fast tiers are never published | `TestFetch_ExcludedSources`, `TestFetch_BatchOnlyPageFails` |
| Unit-priced sections (image, audio, video, tools) and unhandled components skipped | `TestFetch_ExcludedSources` |
| Category-per-group vs model-per-group layouts | `TestFetch`, `TestFetch_PriceConversion` |
| Only the first (standard) panel of a section is read | `TestFetch_OnlyFirstPanelPerSection` |
| Duplicate rows collapse; first wins | `TestFetch_Deduplication` |
| Display name → canonical slug, context annotations stripped | `TestNormalizeSlug` |
| Astro serialization decoding | `TestDecodeAstroValue`, `TestDecodeIslandProps_CharacterReferences` |
| Price validation (`-`, `Free`, zero, negative, NaN, Inf) | `TestPricePerMillion` |
| Short-context column selected over cached/long-context | `TestPriceColumnIndex` |
| Section labels match with or without a trailing period | `TestNormalizeSection`, `TestFetch_SectionWithoutTrailingPeriod` |
| A missing tier prop is a loud failure, not a silent skip | `TestFetch_TextTokenIslandWithoutTierFails` |
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

- **Read the embedded props, not the tables.** The page is an Astro site: each pricing component's data lives in a JSON `props` attribute on `<astro-island>`, and the server-rendered `<table>` is *collapsed* to a few "latest" rows. The previous table scraper broke when the page was redesigned in early 2026 — section headings were renamed ("Text tokens" → "Our latest models") so every table was filtered out, and the reduced DOM would have covered only 4 flagship models. That regression left the source silent for ~190 days until the price-freshness gauges from #196 exposed it (#209). The props carry the full ~40-model standard list and an explicit tier, so neither failure mode applies.
- **Standard tier only.** The page also carries batch, flex and fast (priority) panels. Publishing their prices from the same source would have `openai` disagree with itself between runs. `TextTokenPricingTables` names its tier explicitly; grouped sections are read from the first panel, which the page always renders as standard (`data-content-switcher-initial="standard"`).
- **Only the short-context price is stored.** Several tables publish separate short- and long-context rates. `ScrapedModel` has a single input/output pair, so the short-context input/output columns are used; `priceColumnIndex` matches an explicit label allowlist (`input`/`short context input`, `output`/`short context output`) so cache, long-context, batch and flex columns can never be selected. For `TextTokenPricingTables`, where the props name no columns, the row shape `[name, input, cached input, (cache writes,) output]` is used — input first, output last — and rows with fewer than three values are skipped rather than risk reading a cache value as output.
- **A layout change yields an error, not wrong prices.** `Fetch` errors when zero models survive filtering, so asynq retries and the freshness metric stops advancing rather than the source silently publishing nothing. `TestFetch_EmptyHTML` and `TestFetch_BatchOnlyPageFails` pin that behaviour.
- **A parser break cannot zero out stored prices.** The diff engine ignores models absent from an incoming batch.
- **Untrusted external input.** Prices are validated as finite, positive floats before storage; `NaN`, `Inf`, zero and negatives are rejected.
- **Never writes to the database.** `Fetch` is pure I/O-in, values-out. All persistence goes through the reconciler.
- **Timeouts and SSRF protection are the caller's job** — pass a client with an explicit timeout and `scraper.NewSSRFSafeTransport()`.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/scraper` | `Scraper` interface, `ScrapedModel`, SSRF-safe transport |
| `golang.org/x/net/html` | HTML tokenisation and tree walking |
| `encoding/json` | Decoding the embedded island props |

Consumes nothing else; consumed by `internal/worker` (`HandleOpenAIScrape`).
