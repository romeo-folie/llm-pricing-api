# internal/scraper/litellm

Scraper that fetches LLM model pricing from the [LiteLLM model cost map](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json) hosted on GitHub.

## Purpose

Provides a concrete implementation of the `scraper.Scraper` interface for LiteLLM. The LiteLLM cost map is a community-maintained JSON file that covers a wide range of providers and serves as the **daily cross-reference source** for price reconciliation.

## Structure

```
litellm/
├── scraper.go       — Scraper struct, Fetch implementation, modality mapping
├── scraper_test.go  — Unit tests using httptest.Server
└── README.md
```

## Key Components

### `Scraper`
A thin HTTP client wrapper. Constructed via `New(client *http.Client)` — pass `nil` to get a default 15 s timeout client.

### `Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)`
1. `GET` the raw GitHub JSON URL — the response is a flat `map[string]liteLLMEntry`.
2. Skips any entry where `input_cost_per_token` or `output_cost_per_token` is absent (models without pricing data).
3. Resolves `provider` from `litellm_provider` field; falls back to the prefix before `/` in the model key (e.g. `"gemini/gemini-pro"` → `"gemini"`).
4. Maps the `mode` field to a canonical `Modality` string via `modalityFromMode`:
   - `"embedding"` → `"embedding"`
   - `"image_generation"` → `"image"`
   - `"audio_transcription"` / `"audio_speech"` → `"audio"`
   - `"multimodal"` → `"multimodal"`
   - `"chat"`, `"text_completion"`, `""`, or any unknown → `"text"`

### Error handling
- Non-200 responses drain and discard the body before returning an error (connection pool reuse).
- Context cancellation propagates naturally via `http.NewRequestWithContext`.

## Dependencies

- `internal/scraper` — `ScrapedModel` and `Scraper` interface
- Standard library only (`net/http`, `encoding/json`)

## Usage

```go
s := litellm.New(nil) // uses default 15 s HTTP client
models, err := s.Fetch(ctx)
```
