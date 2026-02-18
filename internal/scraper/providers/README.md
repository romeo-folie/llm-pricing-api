# internal/scraper/providers

Five HTML scrapers that each implement `scraper.Scraper` against a provider's public pricing page, serving as the **ground-truth** source for price reconciliation.

## Purpose

Provider docs pages are the authoritative source for LLM pricing. These scrapers are polled daily and cross-referenced against the OpenRouter and LiteLLM feeds. Any >5% discrepancy between sources triggers a manual review queue entry.

## Structure

```
providers/
├── http.go           — Shared fetchHTML helper (GET, User-Agent, non-200 logging)
├── parse.go          — Generic HTML pricing-table parser (parsePricingTable)
├── openai.go         — OpenAI pricing page scraper
├── anthropic.go      — Anthropic pricing page scraper
├── google.go         — Google AI (Gemini API) pricing page scraper
├── mistral.go        — Mistral AI pricing page scraper
├── amazon.go         — Amazon Bedrock pricing page scraper
├── providers_test.go — Unit tests for all five scrapers
├── testdata/
│   ├── openai.html   — Fixture snapshot of openai.com/api/pricing/
│   ├── anthropic.html— Fixture snapshot of anthropic.com/pricing
│   ├── google.html   — Fixture snapshot of ai.google.dev/pricing
│   ├── mistral.html  — Fixture snapshot of mistral.ai/technology/#pricing
│   └── amazon.html   — Fixture snapshot of aws.amazon.com/bedrock/pricing/
└── README.md
```

## Key Components

### `fetchHTML(ctx, client, url, source)` (`http.go`)
Shared HTTP layer used by all five scrapers:
- Sets `User-Agent: llm-pricing-api/1.0` on every request.
- On non-200: reads a bounded 512-byte body excerpt, logs it at `slog.Warn`, drains the remainder for connection-pool reuse, and returns an error.
- Returns the raw `io.ReadCloser` — caller is responsible for `Close()`.

### `parsePricingTable(r io.Reader, cfg parseTableConfig)` (`parse.go`)
Generic HTML table walker. Walks all `<table>` elements in the document and:
1. Detects `<tbody>` rows (skips `<thead>`).
2. Extracts cell text via recursive text-node concatenation.
3. Reads model name, optional provider, input price, and output price by column index (configured per-provider via `parseTableConfig`).
4. Parses dollar amounts from strings like `"$2.50 / 1M tokens"` — extracts the first `$N.NN` value.
5. Divides prices by 1,000,000 to normalise from per-1M-tokens to per-token.
6. Silently skips rows with unparseable or non-positive prices.

### Provider Scrapers
| File | Source Name | Provider Column | URL |
|---|---|---|---|
| `openai.go` | `openai-docs` | hardcoded `"openai"` | `openai.com/api/pricing/` |
| `anthropic.go` | `anthropic-docs` | hardcoded `"anthropic"` | `anthropic.com/pricing` |
| `google.go` | `google-docs` | hardcoded `"google"` | `ai.google.dev/pricing` |
| `mistral.go` | `mistral-docs` | hardcoded `"mistral"` | `mistral.ai/technology/#pricing` |
| `amazon.go` | `amazon-docs` | lowercased from table col 0 | `aws.amazon.com/bedrock/pricing/` |

Amazon Bedrock is special: its table has four columns (Provider, Model ID, Input, Output), so the `parseTableConfig` sets `providerCol: 0, modelCol: 1`.

## Table Structure Expected

All provider fixtures use the same pattern — the parser is column-index-driven, not structure-driven:

```html
<table>
  <thead><tr><th>Model</th><th>Input</th><th>Output</th></tr></thead>
  <tbody>
    <tr><td>gpt-4o</td><td>$2.50 / 1M tokens</td><td>$10.00 / 1M tokens</td></tr>
  </tbody>
</table>
```

## Dependencies

- `internal/scraper` — `ScrapedModel` and `Scraper` interface
- `golang.org/x/net/html` — HTML tokeniser/tree parser

## Usage

```go
s := providers.NewOpenAI(nil)  // nil → default 30s timeout
models, err := s.Fetch(ctx)

// Amazon needs no special handling — provider is extracted from the HTML table
s2 := providers.NewAmazon(nil)
models2, err2 := s2.Fetch(ctx)
```

## Updating Fixtures

When a provider updates their pricing page layout, update the corresponding `testdata/*.html` file and run:

```bash
go test ./internal/scraper/providers/...
```

The fixture files are the canonical test contract for each provider's HTML structure.
