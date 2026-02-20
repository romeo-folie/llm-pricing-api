# internal/scraper

Data source scrapers for the LLM pricing pipeline.

## Purpose

Defines the `Scraper` interface and `ScrapedModel` type that every scraper implementation must satisfy. Sub-packages implement one scraper each; this parent package contains only the shared contract.

## Structure

```
internal/scraper/
  scraper.go          # Scraper interface and ScrapedModel struct
  ssrf.go             # Shared SSRF-safe HTTP transport and IP validation
  README.md           # This file
  openrouter/         # OpenRouter /v1/models REST API scraper
  litellm/            # LiteLLM GitHub raw JSON scraper
  huggingface/        # HuggingFace Inference Providers scraper (Issue #44)
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
| `SourceName` | constant per scraper | `"openrouter"`, `"litellm"`, or `"huggingface_inference_providers"` |
| `UnderlyingProvider` | slug prefix or API field | Set by pass-through aggregators (OpenRouter, HuggingFace); `""` for direct sources like LiteLLM. Used by the reconciler as an independence gate — two diffs with the same `UnderlyingProvider` do not count as two independent source confirmations. |
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

### SSRF-Safe Transport (`ssrf.go`)

All scrapers fetch from external URLs, making them a potential SSRF (Server-Side Request Forgery) vector — a malicious API response could redirect your server to internal addresses like `169.254.169.254` (cloud metadata) or `localhost:6379` (Redis).

The shared transport closes this attack surface with three exported functions:

| Function | Role |
|----------|------|
| `NewSSRFSafeTransport()` | Returns an `*http.Transport` whose `DialContext` resolves DNS once, validates all IPs against the blocklist, then connects directly to the resolved IP — eliminating the DNS rebinding window between resolution and connection |
| `CheckIPs(addrs)` | Inner validation shared by both the transport and redirect checker. Blocks private (RFC 1918), loopback, link-local, multicast, and unspecified addresses. Fail-closed: unparseable addresses are blocked |
| `CheckRedirectHost(ctx, host)` | `http.Client.CheckRedirect` hook that blocks redirects to private/loopback targets — defense-in-depth for post-connection redirects |

**Usage in scrapers**: Each scraper constructs its default `*http.Client` with `NewSSRFSafeTransport()` as the transport and `CheckRedirectHost` as the redirect policy. Test clients (e.g. backed by `httptest.Server`) bypass this transport since test traffic targets loopback by design.

**Why shared**: Before extraction, each scraper had inline SSRF checks. Adding the HuggingFace scraper as a third consumer made the duplication untenable, so the logic was extracted to `ssrf.go` for all three to share.

## Design Notes

- **Separate from `internal/models`**: `ScrapedModel` is deliberately not in the models package. The reconciler imports both packages; keeping them separate avoids a circular dependency.
- **Prices in per-token units**: all scrapers normalise to per-token cost. Provider pages typically list per-1M-token prices — divide by `1_000_000` before returning.

## Dependencies

| Dependency | Role |
|------------|------|
| `context` (stdlib) | Fetch cancellation and deadline propagation |
| `time` (stdlib) | `FetchedAt` timestamp on `ScrapedModel` |
