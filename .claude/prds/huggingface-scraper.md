---
name: huggingface-scraper
description: Add HuggingFace Inference Providers as a scraper data source with underlying-provider-aware reconciliation
status: backlog
created: 2026-02-20T11:40:43Z
---

# PRD: HuggingFace Inference Providers Scraper

## Executive Summary

Add HuggingFace Inference Providers as a third pricing data source, replacing the five provider-doc scrapers (OpenAI, Anthropic, Google, Mistral, Amazon) that were removed because their SPA-rendered pricing pages cannot be scraped with plain HTTP. HuggingFace exposes pricing for open-weight models (Llama, Mistral, Qwen, Gemma, etc.) hosted across multiple underlying infrastructure providers (Together AI, Replicate, fal-ai, Nebius, Hyperbolic, SambaNova) via a public REST API with no authentication required.

The key complication is that HuggingFace is a **pass-through layer**: its prices reflect what underlying providers charge, not independent HuggingFace pricing. OpenRouter is similarly a pass-through. Agreement between these two sources on a price for the same model at the same infrastructure provider (e.g. Together AI) is **not** independent confirmation — both are reporting the same upstream price. The reconciler must be extended to track this provenance and only treat sources as independent when their underlying infrastructure providers differ.

---

## Problem Statement

### Background

Five provider-doc scrapers were removed from the pipeline because OpenAI, Anthropic, Google, Mistral, and Amazon all serve their pricing pages as JavaScript SPAs. A plain HTTP GET returns a shell HTML document with no pricing data. The five corresponding rows remain in the `sources` table but are effectively dead — no data is ever written against them.

This leaves the platform with only two active data sources: OpenRouter (every 6h) and LiteLLM (daily). Two sources are the minimum required for multi-source reconciliation and the 2-source agreement rule. Losing any additional source would degrade confidence in published prices.

### Why HuggingFace Inference Providers

- **Public machine-readable API** — `GET https://huggingface.co/api/models` returns JSON with `inferenceProviderMapping` containing per-provider pricing. No authentication, no scraping.
- **Wide open-weight model coverage** — Llama 3, Mistral, Qwen, Gemma, Falcon, and other models that are already tracked via OpenRouter and LiteLLM.
- **Multiple infrastructure providers** — Together AI, Replicate, fal-ai, Nebius, Hyperbolic, SambaNova, etc. Some of these overlap with OpenRouter's provider roster, creating meaningful reconciliation opportunities.
- **Stable pricing data** — HuggingFace prices change infrequently (unlike OpenRouter which polls live provider APIs), making daily cadence appropriate.

### Why Now

Phase 5 (Frontend) is the intended next phase, but the data pipeline requires a minimum of three reliable sources to meet the platform's data quality guarantees. Adding HuggingFace closes the gap created by the removed scrapers and strengthens the reconciler's ability to detect discrepancies before they reach the API tier.

---

## User Stories

### Primary Persona: Developer integrating LLM pricing into their product

**As a developer**, I want the pricing data I receive from `/v1/models` and `/v1/models/:id` to have high confidence scores, so that I can trust it for cost estimation without manually cross-checking provider documentation.

*Acceptance criteria:*
- At least some models show `confidence: "high"` after HuggingFace + OpenRouter agree on the same price independently (i.e. via different infrastructure providers)
- Prices marked `confidence: "medium"` are still published and accessible; only review-queue entries are withheld

### Secondary Persona: Platform operator monitoring data quality

**As an operator**, I want the review queue to surface genuine discrepancies — not false positives caused by two sources that both report the same underlying provider's price — so that human review effort is focused on real anomalies.

*Acceptance criteria:*
- If HuggingFace reports model X at Together AI = $0.0009/token, and OpenRouter also reports model X at Together AI = $0.0009/token, this does NOT auto-publish with `confidence: "high"` and does NOT generate a review queue entry (the two sources are non-independent for this model)
- If LiteLLM also reports model X at $0.0009/token, that IS treated as an independent confirmation and results in `confidence: "high"`

### Tertiary Persona: Data analyst querying price history

**As a data analyst**, I want to filter `price_history` by infrastructure provider so I can track how Together AI or Replicate pricing has changed over time across all models they serve.

*Acceptance criteria (stretch goal):*
- `prices` and `price_history` tables have an `underlying_provider` column populated for HuggingFace and OpenRouter records
- API response for `/v1/models/:id` includes `underlying_provider` in price source metadata when available

---

## Requirements

### Functional Requirements

#### FR-1: HuggingFace Scraper

- **FR-1.1** A new scraper package `internal/scraper/huggingface` implements `scraper.Scraper`.
- **FR-1.2** The scraper fetches from `https://huggingface.co/api/models` with query parameters:
  - `inference_provider=all` — returns all providers with pricing
  - `expand[]=inferenceProviderMapping` — includes per-provider pricing in the response
  - `pipeline_tag=text-generation` — restricts to text / chat completion models
  - `sort=likes&direction=-1&limit=500` — prioritise popular models; paginate if needed
- **FR-1.3** For each model, for each entry in `inferenceProviderMapping`, emit one `ScrapedModel` **only if**:
  - Provider status is `"live"` (skip `"inactive"`, `"staging"`, etc.)
  - Both `pricing.input` and `pricing.output` are non-zero positive numbers
  - `pipeline_tag` is `"text-generation"` (already filtered by query param, but re-checked defensively)
- **FR-1.4** Each emitted `ScrapedModel` carries:
  - `Slug`: `{normalizedProvider}/{normalizedModelName}` — provider normalised to match OpenRouter prefix conventions; model name lowercased with underscores/dots converted to hyphens (e.g. HuggingFace `meta-llama/Meta-Llama-3-70B-Instruct` at `together-ai` → slug `together/meta-llama-3-70b-instruct`)
  - `Provider`: normalised provider prefix (e.g. `"together"`)
  - `UnderlyingProvider`: raw HuggingFace provider ID (e.g. `"together-ai"`) — preserved as-is for reconciler independence checking
  - `SourceName`: `"huggingface_inference_providers"`
  - `Modality`: `"text"` (guaranteed by pipeline_tag filter)
  - Pricing in USD per token: from `inferenceProviderMapping[provider].pricing.input` / `.output`
- **FR-1.5** The scraper uses a 60-second HTTP timeout and sets `User-Agent: llm-pricing-api/1.0`.
- **FR-1.6** The scraper must not follow redirects to private IP ranges (SSRF prevention, per security rules).

#### FR-2: Underlying Provider Provenance

- **FR-2.1** `scraper.ScrapedModel` gains a new field `UnderlyingProvider string`. For scrapers that are themselves infrastructure providers (LiteLLM), this is left empty. For pass-through aggregators (HuggingFace, OpenRouter), it identifies the actual infrastructure provider.
- **FR-2.2** OpenRouter's `ScrapedModel` records are updated to set `UnderlyingProvider = providerFromSlug(slug)` (same value as the existing `Provider` field, now explicitly propagated).
- **FR-2.3** `diff.PriceDiff` gains a corresponding `UnderlyingProvider string` field, propagated from `ScrapedModel` through the diff engine.

#### FR-3: Reconciler Independence Check

- **FR-3.1** A new helper `effectiveProvider(d PriceDiff) string` returns `d.UnderlyingProvider` if non-empty, else `d.Source`.
- **FR-3.2** Before routing a multi-source group (2+ diffs for the same slug + field) to `processMultiSource`, count distinct `effectiveProvider` values across the group.
- **FR-3.3** If distinct effective provider count is `< 2`, route to `processSingleSource` instead (using the first diff in the group). The group of diffs represents a single independent data point.
- **FR-3.4** If distinct effective provider count is `≥ 2`, proceed with the existing `processMultiSource` logic unchanged.

#### FR-4: Database Schema

- **FR-4.1** Add source row: `INSERT INTO sources (name, url, type) VALUES ('huggingface_inference_providers', 'https://huggingface.co/api/models', 'api')`.
- **FR-4.2** Add nullable column `underlying_provider TEXT` to `prices` table.
- **FR-4.3** Add nullable column `underlying_provider TEXT` to `price_history` table.
- **FR-4.4** `Store.PublishPrice` accepts an `underlyingProvider string` parameter and writes it to both tables.
- **FR-4.5** Remove stale source rows for `openai`, `anthropic`, `google`, `mistral`, `amazon` (no scraper has written to these since they were removed; the rows are misleading).

#### FR-5: Worker Registration

- **FR-5.1** Task constant `TaskHuggingFaceScrape = "scrape:huggingface"` added to `internal/worker/tasks.go`.
- **FR-5.2** Handler `HandleHuggingFaceScrape` added to `internal/worker/handlers.go`, following the same pattern as `HandleOpenRouterScrape` and `HandleLiteLLMScrape`.
- **FR-5.3** Cron schedule: `@every 24h` (same cadence as LiteLLM, appropriate for slowly-changing HuggingFace pricing).
- **FR-5.4** Initial enqueue on worker startup (same pattern as existing scrapers) so the DB is populated on first deploy without waiting 24 hours.

### Non-Functional Requirements

#### NFR-1: Performance
- Scraper must complete within 60 seconds including HTTP I/O (enforced via client timeout).
- If HuggingFace paginates results, each page fetch must also respect the 60-second total budget via context deadline propagation.

#### NFR-2: Security
- HTTP client must not follow redirects to RFC-1918 private addresses or loopback (SSRF, per security rules).
- No HuggingFace API key is required; the endpoint is public. Do not add any credentials.
- The `underlying_provider` value is derived from HuggingFace's API response. Validate it is a non-empty string before storing; do not store raw untrusted blobs.

#### NFR-3: Observability
- Scraper logs model count per provider on completion (e.g. `"fetched 47 models from 6 providers"`).
- Reconciler logs when a multi-source group is collapsed to single-source due to shared underlying provider (debug level is fine; this is high-volume).
- Failed individual model records are logged at debug level and skipped; the scraper should not abort on a single bad record.

#### NFR-4: Reliability
- A zero-result response (after filtering) is treated as an error so asynq retries. This matches the existing behaviour for OpenRouter and LiteLLM.
- HTTP 429 / 503 from HuggingFace causes the task to fail with an error (asynq retries via its built-in retry mechanism).

---

## Success Criteria

| Metric | Target |
|--------|--------|
| New models ingested from HuggingFace on first run | ≥ 50 distinct slug × provider pairs |
| Models with `confidence: "high"` after 48h | Measurable increase vs. baseline (OpenRouter + LiteLLM only) |
| False-positive review queue entries (same underlying provider) | 0 — the independence check must prevent these |
| Test coverage for new scraper package | ≥ 80% line coverage |
| All existing tests pass | 0 regressions |

---

## Constraints & Assumptions

- **HuggingFace API schema stability** — The `inferenceProviderMapping` field structure is assumed to be stable. If it changes, the scraper will return 0 models (treated as error, task retried).
- **No auth** — The endpoint is public. This assumption must be re-validated before each deployment if HuggingFace restricts access.
- **Slug alignment is best-effort** — HuggingFace provider IDs (e.g. `together-ai`) are normalised to match OpenRouter prefix conventions (e.g. `together`) using a small lookup table. Models where slug normalisation differs between sources will be stored as separate entries and not reconciled against each other. This is acceptable for V1.
- **Text generation scope** — `pipeline_tag=text-generation` is the filter. HuggingFace's classification of models is trusted; no secondary validation of the modality is applied beyond the pipeline_tag check.
- **Pricing unit** — HuggingFace pricing is USD per token (confirmed by API docs). If a model reports pricing in a different unit, there is no detection mechanism in V1; this is an accepted risk.
- **LiteLLM independence** — LiteLLM aggregates prices from its own GitHub-hosted JSON, which is maintained independently of OpenRouter and HuggingFace. It is treated as a fully independent source (empty `UnderlyingProvider`).

---

## Out of Scope

- **Embedding, image, audio, video models** — Different pricing units; excluded by `pipeline_tag=text-generation` filter.
- **HuggingFace-hosted models without Inference Providers** — Models that HuggingFace hosts directly (not via third-party providers) are out of scope for this phase. They may have different pricing structures.
- **Strict slug alignment across sources** — A lookup table that guarantees slug matching between HuggingFace and OpenRouter for every model is not built in V1. It may be added as a follow-on optimisation.
- **Model deduplication UI** — Surfacing "these 3 entries are the same model at different providers" in the API or frontend is not addressed here.
- **Removing stale provider rows from the API response** — The `GET /v1/providers` endpoint may currently return the dead provider rows. Fixing that response is out of scope for this PRD (handled separately).
- **OpenAI / Anthropic / Google pricing** — These provider prices are not available via HuggingFace (only open-weight models). The gap for proprietary model pricing is a separate concern.

---

## Dependencies

### Internal
- `internal/scraper/scraper.go` — `ScrapedModel` struct change (add `UnderlyingProvider` field). Blocked by: nothing.
- `internal/diff/diff.go` — `PriceDiff` struct change. Blocked by: `ScrapedModel` change.
- `internal/reconciler/reconciler.go` — Independence check. Blocked by: `PriceDiff` change.
- `internal/reconciler/store.go` — `PublishPrice` signature change. Blocked by: reconciler change.
- `cmd/worker/main.go` — Registration. Blocked by: handler + task constant.
- Database migration `000008` — Must be applied before the scraper runs.

### External
- HuggingFace Inference Providers API (`https://huggingface.co/api/models`) — Public, no SLA. Monitor for rate limiting or schema changes.

---

## Stretch Goal: API Exposure of `underlying_provider`

If time permits, surface `underlying_provider` in API responses:

- `GET /v1/models/:id` — include `underlying_provider` in the `prices` array (one entry per source), alongside the existing `source`, `confidence`, and `confirmed_at` fields.
- `GET /v1/models/:id/history` — include `underlying_provider` in each history record.
- This requires no new query logic — the column is already on the relevant tables after FR-4.

This is explicitly a stretch goal and must not block the core scraper work.
