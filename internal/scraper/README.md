# internal/scraper

Data source scrapers for the LLM pricing pipeline.

## Purpose

Defines the `Scraper` interface and `ScrapedModel` type that every scraper implementation must satisfy. Sub-packages implement one scraper each; this parent package contains only the shared contract.

## Structure

```
internal/scraper/
  scraper.go          # Scraper interface and ScrapedModel struct
  README.md           # This file
  openrouter/         # OpenRouter /v1/models REST API scraper (Issue #4)
  litellm/            # LiteLLM GitHub raw JSON scraper (Issue #4)
  providers/          # Provider HTML doc scrapers: OpenAI, Anthropic, Google, Mistral, Amazon (Issue #6)
```

## Key Components

### `ScrapedModel`

The normalised output of every scraper. Fields map as follows:

| Field | Source format | Notes |
|-------|--------------|-------|
| `Slug` | model ID string | Canonical identifier matching `models.slug` in DB |
| `Provider` | provider name | e.g. `"openai"`, `"anthropic"` |
| `InputCostPerToken` | varies per source | Normalised to cost per single token (`float64`) |
| `OutputCostPerToken` | varies per source | Normalised to cost per single token (`float64`) |
| `ContextWindow` | integer or nil | `nil` if not reported by source |
| `Modality` | string | Passed through as reported; validated by reconciler |
| `SourceName` | constant per scraper | e.g. `"openrouter"`, `"openai-docs"` |
| `FetchedAt` | time of HTTP request | Set by each scraper at fetch time |

### `Scraper` interface

```go
type Scraper interface {
    Fetch(ctx context.Context) ([]ScrapedModel, error)
}
```

All implementations must:
- Propagate errors (not swallow them) so the asynq worker can retry
- Be safe to call concurrently
- Set `FetchedAt` to the time the HTTP request was made

## Design Notes

- **Separate from `internal/models`**: `ScrapedModel` is deliberately not in the models package. The reconciler imports both packages; keeping them separate avoids a circular dependency.
- **Prices in per-token units**: all scrapers normalise to per-token cost. Provider pages typically list per-1M-token prices — divide by `1_000_000` before returning.

## Dependencies

| Dependency | Role |
|------------|------|
| `context` (stdlib) | Fetch cancellation and deadline propagation |
| `time` (stdlib) | `FetchedAt` timestamp on `ScrapedModel` |
