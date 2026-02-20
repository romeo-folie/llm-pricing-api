---
name: agent-layer
description: MCP server, SSE stream, NL query endpoint, and discovery endpoints enabling AI agents to discover and query LLM pricing data autonomously
status: backlog
created: 2026-02-19T21:19:45Z
updated: 2026-02-19T21:19:45Z
---

# PRD: Agent Layer (Phase 4)

## Executive Summary

Build the interface layer that lets AI agents discover, authenticate with, and query the LLM pricing platform autonomously — without a human opening a browser. This includes an MCP server published to npm as `@llmrates/mcp` (stdio + Streamable HTTP transport), a real-time SSE stream backed by Redis Pub/Sub, a rule-based natural language query endpoint at `/v1/ask`, and a compact context snapshot at `/v1/context`. Discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/llms.txt`) are already stubbed in Phase 2 and will be completed and verified here.

This is the differentiating layer. Every competitor gives a static pricing page; this gives agents first-class programmatic access with structured trust metadata, real-time change streams, and native tool integration in Claude Code and Cursor.

---

## Problem Statement

Phase 2 built a full REST API. Phase 3 built a human-facing frontend. But the highest-value consumers of pricing data are **AI agents** — coding assistants selecting models, cost-optimisation routers, automated procurement systems. Today these agents have no native way to:

- **Discover** the platform exists (no MCP listing, minimal AI plugin manifest)
- **Query** pricing data through tool-use interfaces (no MCP tools)
- **Monitor** price changes in real-time (SSE stub, not connected)
- **Ask** a natural language question and get a structured response (no `/v1/ask`)
- **Load** a compact pricing context into their system prompt (no `/v1/context` implementation)

### Why now

The REST API and data pipeline are complete. Every endpoint this phase needs already exists — the agent layer is a consumer of the Phase 2 API surface, not a parallel build. The MCP ecosystem is growing rapidly (Claude Code, Cursor, Windsurf, Cline all support MCP tools). Shipping early gives us first-mover advantage in agent-native pricing data.

---

## User Stories

### AI Agent (Claude Code / Cursor)
> "As a coding assistant using MCP, I want to call `get_cheapest_model` with task and context parameters so that I can recommend the most cost-effective model for my user's workload without leaving the IDE."

Acceptance criteria:
- MCP tool `get_cheapest_model` accepts `task`, `min_context`, `max_input_price`, `modality` parameters
- Returns ranked results with price, provider, context window, and trust metadata
- Works in Claude Code via `npx @llmrates/mcp` with an API key in MCP config
- Returns a clear error when the API key is missing or invalid

### AI Agent (Automated Router)
> "As an LLM routing agent, I want to subscribe to real-time price changes via SSE so that I can update my routing table within 60 seconds of any confirmed price change."

Acceptance criteria:
- `GET /v1/stream/changes` emits an SSE event within 60 seconds of a confirmed price write
- Stream supports `provider` and `models` query filters
- Reconnection via `Last-Event-ID` resumes from the correct position
- 30-second heartbeat keeps the connection alive through proxies
- Requires Developer+ tier API key

### AI Agent (System Prompt Loader)
> "As an agent framework, I want to fetch a compact pricing snapshot that fits in ≤2,100 tokens so that I can include current LLM prices in my system prompt without blowing my context budget."

Acceptance criteria:
- `GET /v1/context` returns a JSON/markdown snapshot with the top 20 models by usage
- Includes: model ID, provider, input price, output price, context window, confidence
- Total response verified ≤2,100 tokens via tiktoken `cl100k_base` encoding
- Requires Developer+ tier

### Developer (NL Query)
> "As a developer prototyping a cost-analysis feature, I want to ask 'what's the cheapest model with 128k context?' and get a structured JSON response so that I can iterate without memorising filter parameter names."

Acceptance criteria:
- `POST /v1/ask` accepts `{ "query": "..." }` body
- Rule-based parser extracts intent (price, compare, history, recommend) and structured parameters
- Response includes `ranked_models`, `plain_english_summary`, and `inferred_params`
- Unsupported queries return a helpful error with suggested query formats
- Requires Developer+ tier

### MCP Consumer (Pro tier)
> "As a Pro subscriber, I want to call `subscribe_to_changes` via MCP to register a webhook for price change alerts without leaving my IDE."

Acceptance criteria:
- MCP tool `subscribe_to_changes` calls `POST /v1/webhooks` internally
- Accepts a webhook URL, validates it, registers it
- Returns the webhook ID on success
- Requires a Pro-tier API key; returns a tier error for lower tiers

### Platform Operator
> "As an operator, I want the SSE connection count tracked as an OTel gauge so that I can set alerts before hitting memory limits."

Acceptance criteria:
- `active_sse_connections` gauge increments on connect, decrements on disconnect
- Gauge is visible in OTel SDK metrics output (verifiable with local collector or otel-cli)
- Per-key connection limit enforced (max 3 concurrent SSE connections per API key)

---

## Requirements

### Functional Requirements

#### F1 — MCP Server (`@llmrates/mcp`)

##### F1.1 — Project Setup
- TypeScript project in `mcp/` directory
- Built with the official MCP SDK (`@modelcontextprotocol/sdk`)
- Supports both **stdio** transport (for Claude Code, Cursor) and **Streamable HTTP** transport (for browser-based clients)
- API key passed via MCP config environment variable (`LLMRATES_API_KEY`)
- Package name: `@llmrates/mcp`
- Prerequisite: create `@llmrates` npm organisation

##### F1.2 — MCP Tools (6 tools)

| Tool | Parameters | Returns | Tier |
|------|-----------|---------|------|
| `get_cheapest_model` | `task?`, `min_context?`, `max_input_price?`, `modality?`, `top_n?` (default 5) | Ranked models with price, provider, context_window, confidence | Dev+ |
| `compare_models` | `models` (array, max 5) | Side-by-side comparison with price diff and trust metadata | Free |
| `get_price_history` | `model_id`, `from?`, `to?` | Time-series price records with source attribution | Dev+ |
| `get_recent_changes` | `since?` (ISO date), `provider?` | List of confirmed price changes | Free |
| `get_context_snapshot` | `top_n?` (default 20), `format?` (`json`/`markdown`) | Compact pricing snapshot ≤2,100 tokens | Dev+ |
| `subscribe_to_changes` | `webhook_url`, `events?` (default `["price_change"]`) | Webhook registration ID | Pro |

Each tool:
- Maps to an existing `/v1/` REST endpoint (no new business logic)
- Returns structured JSON with trust metadata
- Handles auth errors gracefully with a clear message (not a stack trace)
- Includes a `description` and `inputSchema` for agent discoverability

##### F1.3 — Error Handling
- Invalid API key → `"Authentication failed. Set LLMRATES_API_KEY in your MCP config."`
- Insufficient tier → `"This tool requires {tier} tier. Current: {current_tier}."`
- API unreachable → `"Could not reach the LLM Rates API at {base_url}. Check your network."`
- Validation errors → human-readable message with the specific constraint that failed

##### F1.4 — npm Publishing
- Published to public npm registry as `@llmrates/mcp`
- `npx @llmrates/mcp` launches the stdio server
- `npx @llmrates/mcp --transport http --port 3001` launches Streamable HTTP server
- README with: install instructions, MCP config snippet for Claude Code, MCP config snippet for Cursor, tool reference table, example output

#### F2 — SSE Stream (`GET /v1/stream/changes`)

##### F2.1 — Event Source: Redis Pub/Sub
- The reconciliation engine (`internal/reconciler`) publishes a message to a Redis Pub/Sub channel (`price:changes`) after every confirmed price write
- Message payload: `{ "model_id", "provider", "old_input_price", "new_input_price", "old_output_price", "new_output_price", "confirmed_at", "source", "event_id" }`
- `event_id` is a monotonically increasing integer (Redis INCR on `price:changes:seq`)
- Only **confirmed changes** are published; flagged discrepancies are excluded

##### F2.2 — SSE Endpoint
- `GET /v1/stream/changes` opens an SSE connection
- Query filters: `?provider=`, `?models=` (comma-separated model IDs)
- Each event: `id: {event_id}\ndata: {json_payload}\n\n`
- 30-second heartbeat: `: heartbeat\n\n` (SSE comment, keeps connection alive)
- Requires Developer+ tier API key (validated before stream opens)

##### F2.3 — Reconnection
- Client sends `Last-Event-ID` header on reconnect
- Server replays any events with `event_id > Last-Event-ID` from a Redis sorted set buffer (last 1,000 events, TTL 24h)
- If `Last-Event-ID` is stale (beyond buffer), return all buffered events and continue live

##### F2.4 — Connection Limits
- Max 3 concurrent SSE connections per API key
- Exceeding the limit returns `429 Too Many Requests` with `detail: "Maximum 3 concurrent SSE connections per key"`
- `active_sse_connections` OTel gauge tracks total open connections

#### F3 — Natural Language Query (`POST /v1/ask`)

##### F3.1 — Rule-Based Parser
- No LLM dependency — deterministic regex + keyword extraction
- Four intent categories:

| Intent | Trigger Patterns | Extracted Params |
|--------|-----------------|------------------|
| `price` | "cost of", "price of", "how much", "what does X cost" | `model_id`, `provider` |
| `compare` | "compare", "vs", "versus", "difference between" | `models[]` (2-5) |
| `history` | "history", "changed", "trend", "price drops", "went up" | `model_id`, `since` |
| `recommend` | "cheapest", "best", "recommend", "suggest", "under $X" | `task`, `min_context`, `max_price`, `modality` |

- Parser normalises model names: "gpt4o" → "openai/gpt-4o", "claude sonnet" → "anthropic/claude-3-5-sonnet", "gemini pro" → "google/gemini-pro"
- Ambiguous queries return `400` with `{ "type": "parse_error", "detail": "Could not understand query", "suggestions": ["Try: 'cheapest model with 128k context'", "Try: 'compare gpt-4o vs claude sonnet'"] }`

##### F3.2 — Response Format
```json
{
  "intent": "recommend",
  "inferred_params": {
    "task": null,
    "min_context": 128000,
    "max_input_price": null,
    "modality": "text"
  },
  "ranked_models": [ ... ],
  "plain_english_summary": "The 3 cheapest text models with at least 128k context are: ...",
  "meta": { "query_time_ms": 42, "parser": "rule-v1" }
}
```

##### F3.3 — Tier Gating
- Requires Developer+ tier
- Free tier returns `403` with `tier_required: "developer"`

#### F4 — Context Snapshot (`GET /v1/context`)

##### F4.1 — Implementation
- Returns the top 20 models by provider coverage (most-tracked first), current prices only
- Format: JSON by default, `?format=markdown` for plain-text variant
- Fields per model: `model_id`, `provider`, `input_price_per_1m`, `output_price_per_1m`, `context_window`, `confidence`, `confirmed_at`
- Includes a `generated_at` timestamp and `model_count` in the response wrapper

##### F4.2 — Token Budget
- Total response verified ≤2,100 tokens using tiktoken `cl100k_base` encoding
- If the top 20 models exceed the budget, truncate to fit (drop lowest-priority models)
- Include `token_count` in response metadata so consumers can verify

##### F4.3 — Caching
- Redis cache with 10-minute TTL
- Cache key: `context:v1:{format}` (no per-key variation — same data for all consumers)

#### F5 — Discovery Endpoints (Completion)

##### F5.1 — OpenAPI Spec (`GET /openapi.json`)
- Verify completeness: all Phase 2 + Phase 4 endpoints documented
- Add `/v1/ask`, `/v1/stream/changes` schemas
- Validate against OpenAPI 3.1 schema (no validation errors)

##### F5.2 — AI Plugin Manifest (`GET /.well-known/ai-plugin.json`)
- Update with: correct `name_for_human` ("LLM Rates"), `name_for_model` ("llmrates"), full `description_for_model`, correct `api.url` pointing to OpenAPI spec
- Logo URL pointing to production asset

##### F5.3 — llms.txt (`GET /llms.txt`)
- Plain prose: platform name, what it does, available endpoints, auth instructions, example curl
- Updated to reflect all Phase 4 endpoints

#### F6 — Reconciler Integration (Redis Pub/Sub)

- Modify `internal/reconciler` to publish to Redis channel `price:changes` after every confirmed write
- Generate monotonic `event_id` via Redis INCR on key `price:changes:seq`
- Store last 1,000 events in Redis sorted set `price:changes:buffer` (score = event_id, TTL 24h)
- No changes to existing reconciliation logic — publish is append-only side effect

### Non-Functional Requirements

#### N1 — Performance
- `/v1/context` response time < 100ms (cached)
- `/v1/ask` parsing < 50ms (no LLM call, pure regex)
- SSE event delivery < 60 seconds from reconciler write to client receipt
- MCP tool response time: same as underlying REST endpoint + <50ms MCP overhead

#### N2 — Reliability
- SSE heartbeat every 30 seconds prevents proxy timeouts
- Redis Pub/Sub subscriber auto-reconnects on Redis connection loss
- MCP server handles API timeouts gracefully (5-second timeout per tool call)
- Event replay buffer tolerates Redis restart (events are re-populated from price_history on next reconciler run)

#### N3 — Security
- SSE connections require a valid Developer+ API key validated **before** the stream opens
- `Last-Event-ID` is treated as untrusted input: validated as integer, parameterised in any query
- MCP server never logs the API key value; logs key prefix only (first 8 chars)
- `/v1/ask` input is sanitised: no SQL injection vectors, query string capped at 500 characters
- Webhook URLs registered via MCP `subscribe_to_changes` follow the same SSRF validation as direct API calls (HTTPS only, no private IPs)

#### N4 — Observability
- `active_sse_connections` OTel gauge (increment on connect, decrement on disconnect)
- SSE events emitted counter: `sse_events_emitted_total` (labelled by `provider`)
- `/v1/ask` intent classification counter: `ask_queries_total` (labelled by `intent`)
- MCP tool call counter: `mcp_tool_calls_total` (labelled by `tool_name`)
- All new handlers emit OTel spans via existing `otelfiber` middleware

#### N5 — Testability
- MCP server: end-to-end test — launch server, call each tool via MCP SDK client, verify structured response
- SSE stream: integration test — publish event to Redis, verify SSE client receives it within 5 seconds
- `/v1/ask`: unit tests for each intent pattern with 10+ query variations per intent
- `/v1/context`: unit test verifying token count ≤2,100 with production data fixture
- Reconnection: integration test — connect, receive events, disconnect, reconnect with `Last-Event-ID`, verify replay

---

## Success Criteria

| Metric | Target |
|--------|--------|
| MCP server installable via `npx @llmrates/mcp` | Pass |
| All 6 MCP tools return valid structured data | 100% |
| MCP server works in Claude Code with a Developer tier key | Verified |
| MCP server works in Cursor with a Developer tier key | Verified |
| `/v1/context` response ≤2,100 tokens (cl100k_base) | Verified |
| `/v1/ask` correctly classifies 40+ test queries across 4 intents | ≥90% accuracy |
| `/v1/stream/changes` emits event within 60s of confirmed write | Pass |
| SSE reconnection replays missed events via `Last-Event-ID` | Pass |
| SSE heartbeat every 30 seconds (verified over 5-minute test) | Pass |
| `active_sse_connections` gauge visible in OTel metrics | Pass |
| `/openapi.json` validates against OpenAPI 3.1 with 0 errors | Pass |
| Integration + unit test suite passes with 0 failures | Pass |
| npm package published and accessible via `npx @llmrates/mcp` | Pass |

---

## Constraints & Assumptions

- **Phases 1-2 complete:** All scrapers, reconciliation engine, REST endpoints, auth middleware, and caching are production-ready.
- **No LLM dependency for `/v1/ask`:** The rule-based parser is intentionally deterministic — zero external API calls, zero cost, predictable latency. An LLM-backed parser can be added later as a fallback.
- **Redis already provisioned:** Redis is running on Railway from Phase 1. Pub/Sub and sorted sets use the same instance.
- **OTel Collector not yet running:** New metrics emit via the existing SDK to a no-op endpoint. They activate when Phase 6 brings up the collector.
- **Lemon Squeezy not yet integrated:** Pro-tier keys for `subscribe_to_changes` testing are created manually in Unkey. Real paid provisioning is Phase 5.
- **npm org `@llmrates` must be created** before publishing the MCP package.
- **Token counting:** tiktoken Go/JS binding or equivalent library needed for `/v1/context` budget enforcement.
- **Existing SSE stub:** Phase 2 registered `GET /v1/stream/changes` as a stub. This phase replaces the stub with the full Redis Pub/Sub backed implementation.

---

## Out of Scope

- LLM-backed NL parser for `/v1/ask` (future enhancement — rule-based only in this phase)
- Webhook delivery infrastructure changes (already built in Phase 2; `subscribe_to_changes` MCP tool uses the existing `POST /v1/webhooks`)
- Lemon Squeezy billing integration (Phase 5)
- OTel Collector, Prometheus, Grafana dashboards (Phase 6)
- Admin dashboard (Phase 7)
- Frontend changes (no UI work in this phase)
- MCP resources or prompts (tools only for initial release)
- Multi-language NL parser support (English only)
- Token counting for arbitrary user queries (only for `/v1/context` response budget)

---

## Dependencies

### Internal
- **Phase 2 REST API:** All `/v1/` endpoints that MCP tools consume
- **`internal/reconciler`:** Must be modified to publish Redis Pub/Sub events after confirmed writes
- **`internal/middleware`:** Existing auth, rate limiting, and tier gating middleware
- **`internal/cache`:** Existing Redis client (extended for Pub/Sub and sorted sets)
- **`internal/api/handlers`:** Existing handler patterns; new handlers for `/v1/ask` and `/v1/context` full implementation

### External
- **MCP SDK** (`@modelcontextprotocol/sdk`) — TypeScript SDK for building MCP servers
- **npm registry** — `@llmrates` org for publishing the MCP package
- **Redis** — Pub/Sub channel + sorted set for SSE event buffer (existing Railway instance)
- **tiktoken** — Token counting library for `/v1/context` budget enforcement (Go: `github.com/pkoukk/tiktoken-go` or JS equivalent)

---

## Task Breakdown

| Task | Package | Estimate |
|------|---------|----------|
| Create `@llmrates` npm organisation | npm | 0.5h |
| Set up MCP TypeScript project with MCP SDK, stdio + Streamable HTTP transport | `mcp/` | 3h |
| Implement `get_cheapest_model` MCP tool (calls `/v1/recommend`) | `mcp/` | 2h |
| Implement `compare_models` MCP tool (calls `/v1/compare`) | `mcp/` | 2h |
| Implement `get_price_history` MCP tool (calls `/v1/models/:id/history`) | `mcp/` | 2h |
| Implement `get_recent_changes` MCP tool (calls `/v1/changes`) | `mcp/` | 1.5h |
| Implement `get_context_snapshot` MCP tool (calls `/v1/context`) | `mcp/` | 1.5h |
| Implement `subscribe_to_changes` MCP tool (calls `POST /v1/webhooks`) | `mcp/` | 2h |
| Add API key auth to MCP server (env var `LLMRATES_API_KEY`) | `mcp/` | 1h |
| Write MCP end-to-end tests (all 6 tools, auth errors, tier errors) | `mcp/` | 4h |
| Publish `@llmrates/mcp` to npm with README | `mcp/` | 1h |
| Add Redis Pub/Sub publish to reconciler on confirmed writes | `internal/reconciler` | 3h |
| Add monotonic event_id via Redis INCR (`price:changes:seq`) | `internal/reconciler` | 1h |
| Add event replay buffer: Redis sorted set (`price:changes:buffer`, 1000 events, 24h TTL) | `internal/cache` | 2h |
| Implement full SSE endpoint: Redis Pub/Sub subscriber, event fan-out, query filters | `internal/api/handlers` | 5h |
| Add SSE reconnection via `Last-Event-ID` with replay from buffer | `internal/api/handlers` | 3h |
| Add 30-second heartbeat to SSE stream | `internal/api/handlers` | 1h |
| Add per-key SSE connection limit (max 3) | `internal/middleware` | 2h |
| Add `active_sse_connections` OTel gauge + `sse_events_emitted_total` counter | `internal/otel` | 1.5h |
| Write SSE integration tests (event delivery, reconnection, heartbeat, connection limit) | `internal/api` | 4h |
| Implement rule-based NL parser for `/v1/ask` (4 intents, model name normalisation) | `internal/api/handlers` | 6h |
| Write `/v1/ask` unit tests (40+ query variations across 4 intents) | `internal/api` | 3h |
| Implement full `/v1/context` endpoint with token budget enforcement | `internal/api/handlers` | 4h |
| Write `/v1/context` token count verification test | `internal/api` | 1h |
| Update OpenAPI 3.1 spec with `/v1/ask`, `/v1/stream/changes`, `/v1/context` schemas | `cmd/api` | 2h |
| Update `ai-plugin.json` manifest with correct metadata | `cmd/api` | 0.5h |
| Update `llms.txt` with Phase 4 endpoints and auth instructions | `cmd/api` | 1h |
| Test MCP server in Claude Code with real API key | QA | 1.5h |
| Test MCP server in Cursor with real API key | QA | 1.5h |

**Total:** ~62h coding
