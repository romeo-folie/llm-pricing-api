# internal/api/handlers

HTTP handler functions for the LLM pricing REST API. Every handler function reads from the `Store` interface and writes responses using the shared helpers in `internal/api` (RFC 7807 errors, envelope, trust metadata). No handler function touches the database pool directly.

## Structure

| File | Role |
| --- | --- |
| `store.go` | `Store` interface, all filter/row types, `pgxStore` production implementation |
| `register.go` | `Handlers` struct, `RegisterFree()`, `RegisterDev()`, `RegisterDiscovery()`, `RegisterSSE()`, and `RegisterPro()` route registration helpers |
| `models.go` | `GET /v1/models` (paginated list) and `GET /v1/models/:id` (single model) |
| `providers.go` | `GET /v1/providers` (providers with model counts) |
| `compare.go` | `GET /v1/compare?models=id1,id2,...` (side-by-side pricing, max 5 models) |
| `changes.go` | `GET /v1/changes` (recent price changes, 24h default window) |
| `history.go` | `GET /v1/models/:id/history` (price history with date filters; Developer+ only) |
| `recommend.go` | `GET /v1/recommend` (ranked model suggestions by task/context/price; Developer+ only) |
| `context.go` | `GET /v1/context` (compact pricing snapshot ≤ 2 100 tokens; supports `?format=markdown`; returns `token_count` + `model_count` metadata; Developer+ only) |
| `ask.go` | `POST /v1/ask` (deterministic NL parser → 4 intents: price/compare/history/recommend; `AskHandler` with OTel counter; Developer+ only) |
| `aliases.go` | `ModelAliases` map — ~50 common model name shortcuts → canonical slugs; used by `/v1/ask` parser |
| `discovery.go` | `GET /openapi.json`, `GET /.well-known/ai-plugin.json`, `GET /llms.txt` (public) |
| `sse.go` | `GET /v1/stream/changes` (SSE price-change stream; Developer+ only) |
| `webhooks.go` | `POST /v1/webhooks`, `DELETE /v1/webhooks/:id` (Pro only); `WebhookStore` interface + `pgxWebhookStore`; `WebhookHandlerExport` test shim |
| `billing.go` | `POST /webhooks/lemon-squeezy` handler; `LemonSqueezyHandler`, `LemonSqueezyStore` interface + `pgxLemonSqueezyStore`; `BillingRevokeKeyPayload`; HMAC verification; background goroutine dispatch |
| `handlers_test.go` | Unit tests for Free-tier handlers using Fiber's `app.Test()` and an in-memory mock store |
| `dev_handlers_test.go` | Unit tests for Developer+ handlers (history, recommend, context + markdown/metadata) with tier-gate coverage |
| `ask_test.go` | 46 unit tests for `/v1/ask`: intent classification, alias normalisation, param extraction, response shape |
| `sse_test.go` | Unit tests for SSE stream handler |
| `discovery_test.go` | Unit tests for discovery endpoints |
| `webhooks_test.go` | Unit tests for Pro-tier webhook create/delete handlers with tier-gate and ownership coverage |
| `billing_test.go` | 13 unit tests for `LemonSqueezyHandler`: HMAC verification (valid/bad/missing), idempotency, all 5 event types, same-tier no-op, orphan-key compensation |
| `README.md` | This file |

## Key Components

### Store interface

All database access goes through `Store`. The production implementation (`pgxStore`) is backed by `*pgxpool.Pool`; tests use an in-process `mockStore` that replaces individual method callbacks per test.

```go
store := handlers.NewPgxStore(db)   // production
h := handlers.New(store)            // create Handlers
```

The interface exposes Free-tier methods (`ListModels`, `GetModel`, `ListProviders`, `CompareModels`, `ListChanges`, `GetPriceHistory`) as well as Developer+ methods (`GetModelHistory`, `ListModelsForContext`, `RecommendModels`) used by the history, context, and recommend handlers.

### Handlers struct

`Handlers` holds the `Store` dependency and acts as the receiver for all handler methods. Create one with `handlers.New(store)` and register its methods on a Fiber router.

### Registration helpers

`RegisterFree(v1 fiber.Router, db *pgxpool.Pool, rdb *redis.Client)` constructs the production store and wires all five Free-tier routes onto the supplied router group.

`RegisterDev(v1 fiber.Router, db *pgxpool.Pool, rdb *redis.Client) error` wires the Developer+ routes (`/v1/models/:id/history`, `/v1/recommend`, `/v1/context`, `POST /v1/ask`) with `RequireTier("developer")` middleware applied. Returns an error if OTel instrument creation fails (e.g. for `AskHandler`). Free-tier API keys receive a RFC 7807 403.

`RegisterDiscovery(app *fiber.App, db *pgxpool.Pool, rdb *redis.Client)` wires the three public discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/llms.txt`) on the root Fiber app (no auth required). `rdb` is passed to `DiscoveryHandler` for Redis-cached responses (e.g. `/llms.txt`).

`RegisterSSE(v1 fiber.Router, rdb *redis.Client) error` wires the SSE stream at `/v1/stream/changes` (Developer+ only). `rdb` is the Redis client used for Pub/Sub subscription, replay-buffer access, and per-key connection limiting. Pass `nil` to run in heartbeat-only mode (no live events, no connection limits).

`RegisterPro(v1 fiber.Router, db *pgxpool.Pool, rdb *redis.Client, webhookSecretKey string, log zerolog.Logger)` wires webhook CRUD (`POST /v1/webhooks`, `DELETE /v1/webhooks/:id`) gated behind the Pro tier. `webhookSecretKey` is the hex-encoded 32-byte AES-256-GCM key for encrypting webhook secrets at rest; pass an empty string to use an ephemeral key (secrets will not survive restarts).

`RegisterLemonSqueezy(app *fiber.App, db *pgxpool.Pool, billingSvc *billing.Service, asynqClient *asynq.Client, asynqInspector *asynq.Inspector, signingSecret, variantDev, variantPro string, log zerolog.Logger)` registers `POST /webhooks/lemon-squeezy` on the root Fiber app (outside `/v1` — no API key auth). `billingSvc.Keys` and `billingSvc.Email` are injected for key management and transactional email. `asynqClient` is used to enqueue delayed revoke-key tasks; `asynqInspector` is used to delete pending revoke-key tasks when a subscription is resumed. `signingSecret` is the HMAC-SHA256 key from `LEMONSQUEEZY_SIGNING_SECRET`; an empty string causes all requests to be rejected (fail-closed). `variantDev` / `variantPro` are the Lemon Squeezy variant IDs for the Developer and Pro tiers respectively.

### LemonSqueezyHandler (`billing.go`)

`LemonSqueezyHandler` handles `POST /webhooks/lemon-squeezy`. Key behaviours:

- **HMAC verification**: reads the raw body before any JSON parsing, computes `HMAC-SHA256(signingSecret, body)`, and compares with `X-Signature` using `hmac.Equal` (constant-time). Fails closed: an empty `signingSecret` rejects all requests.
- **Idempotency**: checks `webhook_events` by `webhook_id` (Lemon Squeezy's delivery ID). Duplicate deliveries return 200 immediately with no side effects.
- **Background dispatch**: after recording the event, spawns a goroutine with a 30-second context timeout to run the billing action. The HTTP handler returns 200 immediately regardless of dispatch outcome — errors are logged.
- **Event routing** (`dispatch` method):

| Event | Action |
| --- | --- |
| `subscription_created` | `keys.CreateKey` → `store.CreateSubscription` → `email.SendKeyDelivery`; orphaned key revoked if store fails |
| `subscription_updated` | Resolves current tier; skips entirely if tier unchanged; `keys.UpdateKeyTier` → `store.UpdateSubscriptionTier` → `email.SendPlanChange` |
| `subscription_cancelled` | `store.GetSubscription` → enqueue `billing:revoke-key` at `ends_at` → `store.CancelSubscription` |
| `subscription_expired` | `keys.RevokeKey` → `store.ExpireSubscription` |
| `subscription_resumed` | Cancel pending revoke-key task (best-effort); if subscription was already `expired` (revoke job ran first), `keys.CreateKey` + `store.UpdateSubscriptionKey` + `email.SendKeyDelivery` to restore access → `store.ResumeSubscription` |
| anything else | No-op (returns 200) |

### LemonSqueezyStore (`billing.go`)

```go
type LemonSqueezyStore interface {
    TryInsertWebhookEvent(ctx, eventID, eventType string) (inserted bool, err error)
    CreateSubscription(ctx, lsSubID, email, tier, unkeyKeyID string) error
    GetSubscription(ctx, lsSubID string) (BillingSubscription, error)
    UpdateSubscriptionTier(ctx, lsSubID, tier string) error
    UpdateSubscriptionKey(ctx, lsSubID, unkeyKeyID string) error
    CancelSubscription(ctx, lsSubID, revokeJobID string) error
    ExpireSubscription(ctx, lsSubID string) error
    ResumeSubscription(ctx, lsSubID string) error
}
```

`TryInsertWebhookEvent` uses `INSERT ... ON CONFLICT DO NOTHING` backed by the unique constraint on `event_id`, making idempotency enforcement a single atomic DB round-trip. Returns `(true, nil)` for new events, `(false, nil)` for duplicates.

`NewLemonSqueezyStore(db *pgxpool.Pool) LemonSqueezyStore` returns the production pgx implementation, which maps to the `webhook_events` and `billing_subscriptions` DB tables.

### Trust metadata

Every model response includes a `meta` field with `confirmed_at`, `source`, `confidence`, `age_hours`, and `change_velocity`. These are computed by `api.ComputeTrustMeta()` from the model's `price_history` rows, which are fetched for each model via `store.GetPriceHistory()`.

List endpoints aggregate metadata by choosing the most recently confirmed model's meta as the envelope-level meta value.

### Discovery endpoints (`discovery.go`)

`DiscoveryHandler` serves three public endpoints (no authentication required):

- **`GET /openapi.json`** — returns the compile-time embedded OpenAPI 3.1 document. Embedding at build time eliminates runtime file I/O.
- **`GET /.well-known/ai-plugin.json`** — returns the AI plugin manifest for agent auto-discovery. Key fields:
  - `name_for_human`: `"LLM Rates"` — display name for UI.
  - `name_for_model`: `"llmrates"` — identifier used by LLM agents.
  - `description_for_model`: describes all Phase 4 agent capabilities including `/v1/ask`, `/v1/context`, and `/v1/stream/changes`.
  - `api.url`: `"/openapi.json"` — points agents to the OpenAPI spec.
- **`GET /llms.txt`** — returns a plain-text document suitable for agent context loading. It has two sections:
  1. A **static header** with the base URL, authentication instructions, a full endpoint listing, and example `curl` commands.
  2. A **dynamic price listing** fetched from the DB: one line per model in the format `{provider}/{slug}: input=$N.NNNN/1M output=$N.NNNN/1M`.

### SSE stream handler (`sse.go`)

`SSEHandler` manages GET `/v1/stream/changes`. Key behaviours:

- **Constructor**: `NewSSEHandler(rdb *redis.Client) (*SSEHandler, error)` — accepts an optional Redis client. When `rdb` is `nil` the handler operates in heartbeat-only mode (sends `": ok"` on connect, then `": heartbeat"` every 30 seconds). When `rdb` is non-nil, full Pub/Sub subscription, replay-buffer access, and per-key connection limiting are enabled.

- **Query filters**: `?provider=<name>` filters events by provider (case-insensitive). `?models=<id1>,<id2>,...` filters events to only the listed model IDs.

- **`Last-Event-ID` reconnection**: If the client sends a `Last-Event-ID` header the handler replays all events from the `price:changes:buffer` sorted set whose score is greater than the given ID, applying the same provider/model filters. Non-integer or negative values are rejected with 400 before any stream is opened.

- **Per-key connection limit**: Each API key (identified by its SHA-256 hash stored in `c.Locals("key_hash")`) may hold at most 3 concurrent SSE connections. The count is tracked in Redis under `sse:conn:<key_hash>` with a 5-minute safety-net TTL. A fourth concurrent connection receives 429. The counter is decremented in a `defer` inside the writer goroutine to match the actual goroutine lifetime.

- **OTel metrics**:
  - `llm_pricing.sse.active_connections` (UpDownCounter) — live connection count.
  - `llm_pricing.sse.events_emitted_total` (Counter) — events sent, labelled by `provider`.

### Error responses

All errors are RFC 7807 `ProblemDetail` objects returned by the constructors in `internal/api/problem.go`:

| Situation | Constructor |
| --- | --- |
| Invalid query param | `api.NewBadRequest(detail)` |
| Resource not found | `api.NewNotFound(detail)` |
| Database error | `api.NewInternalError(detail)` |

The Fiber `ErrorHandler` in `internal/api/problem.go` serialises all returned errors with `Content-Type: application/problem+json`.

## Dependencies

- `internal/api` — `ProblemDetail`, `TrustMeta`, `ComputeTrustMeta`, `OK`, `Envelope`
- `internal/billing` — `KeyManager`, `Emailer` interfaces consumed by `LemonSqueezyHandler`
- `internal/models` — `Confidence` constants
- `github.com/gofiber/fiber/v2` — HTTP framework
- `github.com/hibiken/asynq` — used by `LemonSqueezyHandler` for billing task enqueueing/deletion
- `github.com/jackc/pgx/v5/pgxpool` — production DB pool (only in `store.go`, `register.go`, and `billing.go`)
- `github.com/redis/go-redis/v9` — passed through `RegisterFree`/`RegisterDev` for future use (currently unused in handler logic, caching is handled at middleware layer)

## Usage

Wire routes from `cmd/api/main.go`:

```go
import "llm-pricing-api/internal/api/handlers"

// v1 is the existing fiber.Router with Auth, RateLimit, Cache middleware.
handlers.RegisterFree(v1, db, redisClient)
if err := handlers.RegisterDev(v1, db, redisClient); err != nil {
    log.Fatal().Err(err).Msg("failed to register dev handlers")
}
if err := handlers.RegisterSSE(v1, redisClient); err != nil {
    log.Fatal().Err(err).Msg("SSE handler setup failed")
}
// webhookSecretKey: hex-encoded 32-byte AES-256-GCM key from config; empty = ephemeral key
handlers.RegisterPro(v1, db, redisClient, cfg.WebhookSecretKey, logger)

// Discovery routes — no auth, registered on the root app, not the v1 group.
handlers.RegisterDiscovery(app, db, redisClient)

// Lemon Squeezy webhook — no API key auth; HMAC-verified; outside /v1 group.
if billingSvc != nil {
    handlers.RegisterLemonSqueezy(
        app, db, billingSvc,
        asynqClient, asynqInspector,
        cfg.LSSigningSecret, cfg.LSVariantDev, cfg.LSVariantPro,
        log,
    )
}
```

### Developer+ endpoints

All Developer+ handlers enforce the tier gate via `RequireTier("developer")` at the route level:

| Endpoint | Handler | Behaviour |
| --- | --- | --- |
| `GET /v1/models/:id/history` | `GetModelHistory` | Full price history with optional `?from=` and `?to=` ISO 8601 filters, descending `confirmed_at` order; 404 if model missing, 400 for bad params |
| `GET /v1/recommend` | `Recommend` | Ranked model list; maps `?task=` to modality filter, filters by `?context=` and `?max_price_input=`; post-fetch modality filtering keeps the Store interface simple |
| `GET /v1/context` | `GetContext` | Compact pricing snapshot capped at 2 100 tokens; `?format=markdown` returns a GitHub-flavoured markdown table; response always includes `token_count` and `model_count` metadata |
| `POST /v1/ask` | `AskHandler.Ask` | Natural language query → structured intent + params; deterministic regex cascade (no LLM); 500-char cap; 400 with `suggestions` on no-match; OTel counter `llm_pricing.ask.queries_total` labelled by intent |

### /v1/ask NL Parser

`POST /v1/ask` accepts `{ "query": "..." }` and classifies it into one of four intents using a priority-ordered regex cascade:

1. **compare** — "vs", "versus", "compare", "difference between"
2. **recommend** — "cheapest", "cheap", "recommend", "suggest", "best model", "under $", "optimal", "most affordable"
3. **history** — "history", "historical", "changed", "trend", "price drop", "went up", "over time", "last week/month"
4. **price** — "cost of", "price of", "how much", "pricing for", "costs", "priced at"

**Parameter extraction** per intent:
- `price`: model name via `ModelAliases` map (longest match wins)
- `compare`: all model aliases found in query → `inferred_params.models` array
- `history`: model alias + time reference (last week=7d, last month=30d, last year=365d, "past N days")
- `recommend`: `max_price` from "under $N", `context_window` from "NNNk", `task` keyword

**Response** (`price` intent example):
```json
{
  "data": {
    "intent": "price",
    "inferred_params": { "model": "openai/gpt-4o" },
    "models": [],
    "plain_english_summary": "Check the current pricing for openai/gpt-4o via GET /v1/models with a provider filter.",
    "meta": { "query_time_ms": 5, "parser": "rule-v1" }
  },
  "meta": {}
}
```

**Response** (`recommend` intent example):
```json
{
  "data": {
    "intent": "recommend",
    "inferred_params": { "task": "summarization", "max_price": 5.0 },
    "ranked_models": [ { "id": 3, "provider": "openai", "slug": "gpt-4o-mini", ... } ],
    "plain_english_summary": "Found 3 model(s) matching your criteria. Top recommendation: gpt-4o-mini (input: $0.1500/1M tokens).",
    "meta": { "query_time_ms": 12, "parser": "rule-v1" }
  },
  "meta": {}
}
```

- `intent`: one of `"price"`, `"compare"`, `"history"`, `"recommend"`
- `inferred_params`: parameters extracted from the query (model slug, max_price, context_window, task, days, models list)
- `models`: populated for `price` and `compare` intents (currently empty — callers use `inferred_params` to fetch)
- `ranked_models`: populated for `recommend` intent — calls `store.RecommendModels` for live DB results
- `plain_english_summary`: human-readable result summary including suggested follow-up API calls
- `meta.query_time_ms`: total handler latency including any DB round-trip
- `meta.parser`: parser version (`"rule-v1"` — deterministic regex, no LLM involved)

### ModelAliases (`aliases.go`)

`ModelAliases` is a `map[string]string` with ~50 lowercase alias → canonical slug entries. Covers OpenAI (gpt4, gpt4o, o1, o3-mini), Anthropic (claude, claude sonnet/opus/haiku variants), Google (gemini/gemini flash/pro), Meta (llama 3.x), Mistral, and DeepSeek. The parser uses longest-match semantics.

### /v1/context enhancements

`GET /v1/context` now supports two additional behaviours:

1. **`?format=markdown`** — returns `Content-Type: text/markdown` with a header line `> Models: N | Estimated tokens: N` followed by a GitHub-flavoured markdown table (slug, provider, input/output $/1M, confidence).
2. **`token_count` + `model_count`** — the JSON response now includes these fields alongside `models`.

Existing token-budget trimming (≤ 2 100 tokens) and all prior JSON response fields are unchanged.

#### Task → modality mapping (for `/v1/recommend`)

| `?task=` | Modality filter applied |
| --- | --- |
| `summarisation`, `summarization`, `text`, `classification`, `generation` | `text` |
| `vision`, `multimodal` | `multimodal` |
| `image` | `image` |
| `audio` | `audio` |
| `embedding`, `embeddings` | `embedding` |
| anything else | none (all modalities returned) |

## Running Tests

```bash
# All handler tests (no live database required)
go test ./internal/api/handlers/...

# Verbose output
go test -v ./internal/api/handlers/...

# With coverage
go test -cover ./internal/api/handlers/...
```

Tests use Fiber's `app.Test()` with an in-memory `mockStore`. Each test overrides only the store method it exercises, keeping tests focused and independent.
