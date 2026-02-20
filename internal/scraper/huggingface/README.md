# internal/scraper/huggingface

HuggingFace Inference Providers scraper — the third active pricing data source.

## Purpose

Fetches all `text-generation` models from the [HuggingFace Inference Providers API](https://huggingface.co/api/models)
and normalises each live model×provider pair to a `scraper.ScrapedModel`. The scraper runs daily
and feeds into the diff engine → reconciler pipeline alongside OpenRouter and LiteLLM.

## Structure

```
internal/scraper/huggingface/
  scraper.go      # Scraper type, New(), Fetch(), SSRF-safe Transport, provider/model normalisation
  scraper_test.go # Unit tests (96.8% coverage) — uses httptest; tests SSRF prevention directly
  README.md       # This file
```

## Key Components

### `Scraper` struct

```go
type Scraper struct {
    client  *http.Client
    baseURL string
}
```

### `New(client *http.Client) *Scraper`

Returns a `Scraper` using the provided HTTP client. If `client` is `nil`, a production-safe
default client is created with:

- **60s timeout** — no unbounded requests per CLAUDE.md scraper safety rules.
- **SSRF-safe Transport** (`newSSRFSafeTransport`) — resolves the target hostname once in
  `DialContext`, checks all resolved IPs against RFC-1918 / loopback / link-local /
  multicast / unspecified blocklists, and connects directly to the first safe IP (eliminates
  the DNS rebinding window between resolution and TCP connect).
- **`CheckRedirect` defense-in-depth** — blocks redirects to private/loopback hostnames
  before a TCP connection is even attempted.

Pass a custom client (e.g. `srv.Client()`) in tests to bypass the SSRF Transport.

### `Fetch(ctx context.Context) ([]scraper.ScrapedModel, error)`

Queries `https://huggingface.co/api/models` with:
```
?inference_provider=all&expand[]=inferenceProviderMapping&pipeline_tag=text-generation&sort=likes&direction=-1&limit=500
```

For each model × inference-provider pair it applies these filters (any mismatch skips the entry):

| Filter | Rule |
|--------|------|
| `pipeline_tag` | Must be `"text-generation"` |
| `status` | Must be `"live"` |
| `pricing.input` | Must be `> 1e-12` |
| `pricing.output` | Must be `> 1e-12` |

Surviving entries are mapped to `scraper.ScrapedModel`:

| Field | Value |
|-------|-------|
| `Slug` | `normProv + "/" + normalizeModelName(hfModelID)` |
| `Provider` | `normalizedProvider(hfProviderID)` |
| `UnderlyingProvider` | `normalizedProvider(hfProviderID)` — same as Provider so it matches OpenRouter's convention (e.g. `"together"` not `"together-ai"`) |
| `InputCostPerToken` | `entry.Pricing.Input` (already per-token from HF) |
| `OutputCostPerToken` | `entry.Pricing.Output` |
| `Modality` | `"text"` |
| `SourceName` | `"huggingface_inference_providers"` |
| `FetchedAt` | `time.Now().UTC()` at fetch time |

Returns an error if the HTTP request fails, response is non-200, JSON decoding fails, or no
models remain after filtering.

### Provider normalisation (`normalizedProvider`)

Maps HuggingFace provider IDs to the canonical short names used elsewhere in the pipeline.
Unknown IDs pass through unchanged (best-effort mapping).

| HF ID | Canonical name |
|-------|---------------|
| `together-ai` | `together` |
| `fireworks-ai` | `fireworks` |
| `fal-ai` | `fal-ai` |
| `replicate` | `replicate` |
| `nebius` | `nebius` |
| `hyperbolic` | `hyperbolic` |
| `sambanova` | `sambanova` |
| `hf-inference` | `hf-inference` |

### Model name normalisation (`normalizeModelName`)

Strips the org prefix, lowercases, and replaces `.` and `_` with `-`:
```
"meta-llama/Meta-Llama-3-70B-Instruct" → "meta-llama-3-70b-instruct"
"mistralai/Mistral-7B-Instruct-v0.3"   → "mistral-7b-instruct-v0-3"
```

### SSRF prevention (`checkIPs`, `checkRedirectHost`, `newSSRFSafeTransport`)

Three-layer defense:

1. **`checkIPs(addrs []string) error`** — shared inner check. Blocks loopback, private,
   link-local unicast, link-local multicast, and unspecified addresses. Fails closed on
   unparseable address strings.
2. **`checkRedirectHost(ctx, host)`** — resolves DNS and calls `checkIPs`; used by
   `CheckRedirect` to abort redirects early.
3. **`newSSRFSafeTransport()`** — resolves DNS in `DialContext`, calls `checkIPs`, then
   connects to the first safe resolved IP directly (no second resolution).

## Dependencies

| Dependency | Role |
|------------|------|
| `llm-pricing-api/internal/scraper` | `ScrapedModel` type (output contract) |
| `context` (stdlib) | Cancellation propagation |
| `encoding/json` (stdlib) | Decode HF API response |
| `net` (stdlib) | `Dialer`, `Resolver`, IP classification for SSRF checks |
| `net/http` (stdlib) | HTTP client and transport |
| `time` (stdlib) | `FetchedAt` timestamp, client/dialer timeouts |

## Usage

```go
// Production (passed to the worker handler):
s := huggingface.New(nil)  // uses SSRF-safe default client

// Worker handler:
models, err := s.Fetch(ctx)
if err != nil {
    // asynq will retry — propagate the error
    return err
}

// Testing (bypass SSRF Transport):
srv := httptest.NewServer(handler)
s := &huggingface.Scraper{} // use newTestScraper(srv) helper in tests
```

> **Note:** the scraper is wired into the worker in Issue #46. Until then, `New` and `Fetch`
> are called only from tests.
