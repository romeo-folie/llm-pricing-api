---
name: data-pipeline
description: Implement scrapers, diff engine, reconciliation engine, and review queue for the LLM pricing data pipeline
status: complete
created: 2026-02-18T03:30:47Z
updated: 2026-02-18T11:37:42Z
---

# PRD: Data Pipeline (Phase 1)

## Executive Summary

Implement the core data pipeline that ingests LLM pricing data from three independent sources (OpenRouter, LiteLLM, provider docs), diffs incoming values against stored values, reconciles discrepancies using a 2-source agreement rule, and writes every confirmed change as an immutable timestamped record in TimescaleDB. This is the foundational data layer that every subsequent phase depends on.

Infrastructure prerequisites (PostgreSQL + TimescaleDB, Redis, asynq, schema migrations) are already in place from Phase 0. This PRD covers only the coding work.

---

## Problem Statement

LLM token pricing changes frequently and inconsistently across providers. Existing tools give a point-in-time snapshot; there is no reliable programmatic source of truth for price history or change velocity. The data pipeline must produce a trustworthy, source-attributed, reconciled dataset that downstream API consumers can depend on.

### Why now
The REST API (Phase 2), frontend (Phase 3), and MCP server (Phase 4) all read from this pipeline. Correctness bugs here propagate into every downstream layer and are extremely expensive to fix retroactively.

---

## User Stories

### Automated Scraper Consumer
> "As the reconciliation engine, I want to receive a normalized list of models and prices from each scraper so that I can compare them against stored values without knowing how each source was fetched."

Acceptance criteria:
- Each scraper implements a common `Scraper` interface returning `[]ScrapedModel`
- Scraped data includes: model ID, provider, input price per 1M tokens, output price per 1M tokens, context window, modality, source name, fetched-at timestamp
- Scraper errors are propagated (not swallowed) so asynq can retry

### Diff Engine Consumer
> "As the reconciliation engine, I want to know exactly which models have changed and by how much so that I can decide whether to auto-publish or flag for review."

Acceptance criteria:
- Diff engine accepts current DB state and incoming scraped state
- Returns a list of diffs: `{model_id, field, old_value, new_value, pct_change}`
- Diffs with `pct_change > 5%` are marked as requiring reconciliation (not auto-published)

### Reconciliation Engine Consumer
> "As the system, I want the reconciliation engine to be the sole writer to `price_history` so that no scraper can directly mutate the price record."

Acceptance criteria:
- Reconciliation engine is the only code path that INSERTs into `price_history`
- 2-source agreement on a value → auto-publish (insert to `price_history`, update `models.current_price`)
- Single-source change → hold, re-check on next fetch cycle; auto-publish after 2 consecutive matching fetches
- Source disagreement > 5% → insert into `review_queue`, do NOT publish
- All `price_history` records include: model_id, price_input, price_output, source, confirmed_at, confidence

### Ops / Admin Consumer
> "As an operator, I want a simple review queue interface so that I can see flagged discrepancies, compare source values, and approve or reject them."

Acceptance criteria:
- Review queue shows: model name, provider, old value, new value, flagging source(s), timestamp
- Operator can mark a record as approved (→ publish to `price_history`) or rejected (→ discard)
- No JavaScript framework required — plain HTML form + Go template is sufficient

---

## Requirements

### Functional Requirements

#### F1 — OpenRouter Scraper
- Fetch `GET /v1/models` from OpenRouter API every 6 hours via asynq cron job
- Parse response into `[]ScrapedModel`
- Register as an asynq task in `cmd/worker`
- Exponential backoff on failure (3 retries max)

#### F2 — LiteLLM Scraper
- Fetch the LiteLLM model cost map JSON from GitHub (raw URL) daily via asynq cron
- Parse into `[]ScrapedModel`
- Handle model ID normalization (LiteLLM uses `provider/model-name` format)

#### F3 — Provider Doc Scrapers
- One scraper each for: OpenAI, Anthropic, Google, Mistral, Amazon
- Fetched daily via asynq cron
- Scraper logic kept thin: parse known page structure, fail gracefully if structure changes
- On parse failure: log the error, skip the affected provider for that cycle, do not crash

#### F4 — Diff Engine
- Accepts: stored `[]Model` (from DB) + incoming `[]ScrapedModel`
- Returns: `[]PriceDiff` with fields: `model_id`, `field`, `old_value`, `new_value`, `pct_change`, `source`
- Pure function — no DB reads or writes; fully unit-testable in isolation

#### F5 — Reconciliation Engine
- Consumes diffs from the diff engine
- Implements the 2-source agreement rule
- Writes to `price_history` (append-only, no UPDATE/DELETE)
- Writes to `review_queue` for flagged discrepancies
- Updates `models` table current price only after publishing to `price_history`

#### F6 — Review Queue
- Simple HTML admin page at `/admin/review`
- Lists all open `review_queue` records with source values side-by-side
- Approve button: writes to `price_history`, marks queue record resolved
- Reject button: marks queue record discarded, no price change
- No auth required in Phase 1 (internal tool only)

#### F7 — Immutable History
- Every confirmed price change → one new row in `price_history`
- No updates or deletes on `price_history` ever
- Row includes: `model_id`, `price_input`, `price_output`, `source`, `confirmed_at`, `confidence` (high/medium/low)

### Non-Functional Requirements

#### N1 — Testability
- Reconciliation engine: >80% unit test coverage
- Diff engine: 100% unit test coverage (it's a pure function)
- Scrapers: integration tests that mock HTTP responses (no live API calls in CI)

#### N2 — Observability
- Each asynq task logs: task name, start time, end time, models fetched count, diffs detected count, errors
- Structured logging (JSON) via `slog`

#### N3 — Reliability
- Scraper failures must not block other scrapers (run independently)
- A single bad scraper result must not corrupt the `price_history` table
- asynq retry handles transient network failures

#### N4 — Performance
- Scraper + diff + reconciliation cycle must complete within 30 seconds per source
- Review queue page must load within 500ms (simple query, no heavy joins)

---

## Success Criteria

| Metric | Target |
|--------|--------|
| All 3 scraper workers running without errors | 48 consecutive hours |
| Models with price data in DB | ≥150 |
| Manually injected discrepancy flagged by reconciliation engine | 100% detection rate |
| `price_history` row created on every confirmed change | Verified by integration test |
| Unit test coverage on reconciliation engine | >80% |
| Unit test coverage on diff engine | 100% |

---

## Constraints & Assumptions

- **Infrastructure is ready:** PostgreSQL + TimescaleDB, Redis, asynq, and all migrations from Phase 0 are assumed to be working. This PRD does not cover any infra work.
- **No auth on admin UI:** Review queue is internal-only in Phase 1; auth is deferred to Phase 2.
- **No REST API:** Phase 1 produces no externally-facing endpoints. That's Phase 2.
- **No UI framework:** Admin review page is plain Go HTML templates.
- **Provider doc format may change:** Scrapers must fail gracefully; the pipeline must not crash if one provider's page structure changes.
- **OpenRouter is the primary feed:** OpenRouter data takes precedence in conflict resolution logic when it agrees with at least one other source.

---

## Out of Scope

- REST API endpoints (`/v1/*`) — Phase 2
- Authentication / API key management — Phase 2
- Frontend (Next.js) — Phase 3
- MCP server — Phase 4
- Webhook delivery — Phase 2
- Railway production deployment — Phase 2
- Provider doc scrapers beyond the 5 listed (OpenAI, Anthropic, Google, Mistral, Amazon)
- Automated fixing of flagged review queue items (always requires human approval in Phase 1)
- Price history data backfill from before Phase 1

---

## Dependencies

### Internal (Phase 0 deliverables — already complete)
- PostgreSQL + TimescaleDB running locally via Docker
- Redis running locally via Docker
- asynq worker scaffold in `cmd/worker`
- DB migrations for: `models`, `prices`, `price_history`, `sources`, `review_queue`
- `internal/scraper`, `internal/reconciler`, `internal/models` package scaffolds
- Config management (`internal/config`)

### External
- OpenRouter API (`/v1/models`) — public, no auth required for model listing
- LiteLLM model cost map — public GitHub raw JSON
- Provider pricing pages — public HTML pages (OpenAI, Anthropic, Google, Mistral, Amazon)

---

## Task Breakdown (Coding Only)

| Task | Package | Estimate |
|------|---------|----------|
| Define `Scraper` interface and `ScrapedModel` type | `internal/scraper` | 1h |
| Implement OpenRouter scraper | `internal/scraper/openrouter` | 4h |
| Implement LiteLLM scraper | `internal/scraper/litellm` | 3h |
| Implement provider doc scrapers (5 providers) | `internal/scraper/providers` | 8h |
| Register scraper asynq tasks + cron schedule | `cmd/worker`, `internal/worker` | 2h |
| Implement diff engine | `internal/diff` | 5h |
| Implement reconciliation engine | `internal/reconciler` | 6h |
| Implement review queue DB layer + admin HTTP handler | `internal/review`, `cmd/api` | 3h |
| Write unit tests: diff engine | `internal/diff` | 2h |
| Write unit tests: reconciliation engine | `internal/reconciler` | 4h |
| Write integration tests: scrapers (mocked HTTP) | `internal/scraper` | 2h |

**Total:** ~40h coding
