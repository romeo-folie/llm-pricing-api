# internal/scraper/openrouter

Scraper that fetches LLM model pricing from the [OpenRouter REST API](https://openrouter.ai/api/v1/models).

## Purpose

Provides a concrete implementation of the `scraper.Scraper` interface for OpenRouter. OpenRouter aggregates hundreds of models from multiple providers and exposes their prices via a single endpoint, making it the **primary feed** for this platform (polled every 6 hours by the asynq worker).

## Structure

```
openrouter/
├── scraper.go       — Scraper struct, Fetch implementation, JSON types
├── scraper_test.go  — Unit tests using httptest.Server
└── README.md
```

## Key Components

### `Scraper`
A thin HTTP client wrapper. Constructed via `New(client *http.Client)` — pass `nil` to get a default 15 s timeout client.

### `Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)`
1. `GET {baseURL}/models` — deserialises the `{"data": [...]}` envelope.
2. Parses `pricing.prompt` and `pricing.completion` as `float64` (prices are returned as decimal strings by OpenRouter).
3. **Skips models with zero or unparseable prices.** OpenRouter represents free/open-weight models as `"0"`. Phase 1 focuses on commercially-priced models; free models are out of scope.
4. Extracts `provider` from the `"provider/model"` slug prefix.
5. Passes `architecture.modality` through verbatim (OpenRouter already uses consistent values like `"text"`, `"multimodal"`).

### Error handling
- Non-200 responses drain and discard the body before returning an error (connection pool reuse).
- Context cancellation propagates naturally via `http.NewRequestWithContext`.

## Dependencies

- `internal/scraper` — `ScrapedModel` and `Scraper` interface
- Standard library only (`net/http`, `encoding/json`, `strconv`)

## Usage

```go
s := openrouter.New(nil) // uses default 15 s HTTP client
models, err := s.Fetch(ctx)
```
