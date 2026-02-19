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
| `context.go` | `GET /v1/context` (compact pricing snapshot ≤ 2 100 tokens for agents; Developer+ only) |
| `discovery.go` | `GET /openapi.json`, `GET /.well-known/ai-plugin.json`, `GET /llms.txt` (public) |
| `sse.go` | `GET /v1/stream/changes` (SSE price-change stream; Developer+ only) |
| `webhooks.go` | `POST /v1/webhooks`, `DELETE /v1/webhooks/:id` (Pro only); `WebhookStore` interface + `pgxWebhookStore`; `WebhookHandlerExport` test shim |
| `handlers_test.go` | Unit tests for Free-tier handlers using Fiber's `app.Test()` and an in-memory mock store |
| `dev_handlers_test.go` | Unit tests for Developer+ handlers (history, recommend, context) with tier-gate coverage |
| `sse_test.go` | Unit tests for SSE stream handler |
| `discovery_test.go` | Unit tests for discovery endpoints |
| `webhooks_test.go` | Unit tests for Pro-tier webhook create/delete handlers with tier-gate and ownership coverage |
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

`RegisterDev(v1 fiber.Router, db *pgxpool.Pool, rdb *redis.Client)` wires the three Developer+ routes (`/v1/models/:id/history`, `/v1/recommend`, `/v1/context`) with `RequireTier("developer")` middleware applied. Free-tier API keys receive a RFC 7807 403 with `{"tier_required": "developer"}`.

`RegisterDiscovery(app *fiber.App, db *pgxpool.Pool)` wires the three public discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/llms.txt`) on the root Fiber app (no auth required).

`RegisterSSE(v1 fiber.Router, rdb *redis.Client) error` wires the SSE stream at `/v1/stream/changes` (Developer+ only). `rdb` is the Redis client used for Pub/Sub subscription, replay-buffer access, and per-key connection limiting. Pass `nil` to run in heartbeat-only mode (no live events, no connection limits).

`RegisterPro(v1 fiber.Router, db *pgxpool.Pool, rdb *redis.Client)` wires webhook CRUD (`POST /v1/webhooks`, `DELETE /v1/webhooks/:id`) gated behind the Pro tier.

### Trust metadata

Every model response includes a `meta` field with `confirmed_at`, `source`, `confidence`, `age_hours`, and `change_velocity`. These are computed by `api.ComputeTrustMeta()` from the model's `price_history` rows, which are fetched for each model via `store.GetPriceHistory()`.

List endpoints aggregate metadata by choosing the most recently confirmed model's meta as the envelope-level meta value.

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
- `internal/models` — `Confidence` constants
- `github.com/gofiber/fiber/v2` — HTTP framework
- `github.com/jackc/pgx/v5/pgxpool` — production DB pool (only in `store.go` and `register.go`)
- `github.com/redis/go-redis/v9` — passed through `RegisterFree`/`RegisterDev` for future use (currently unused in handler logic, caching is handled at middleware layer)

## Usage

Wire routes from `cmd/api/main.go`:

```go
import "llm-pricing-api/internal/api/handlers"

// v1 is the existing fiber.Router with Auth, RateLimit, Cache middleware.
handlers.RegisterFree(v1, db, redisClient)
handlers.RegisterDev(v1, db, redisClient)
if err := handlers.RegisterSSE(v1, redisClient); err != nil {
    log.Fatal().Err(err).Msg("SSE handler setup failed")
}
handlers.RegisterPro(v1, db, redisClient)

// Discovery routes — no auth, registered on the root app, not the v1 group.
handlers.RegisterDiscovery(app, db)
```

### Developer+ endpoints

The three Developer+ handlers enforce the tier gate via `RequireTier("developer")` at the route level:

| Endpoint | Handler | Behaviour |
| --- | --- | --- |
| `GET /v1/models/:id/history` | `GetModelHistory` | Full price history with optional `?from=` and `?to=` ISO 8601 filters, descending `confirmed_at` order; 404 if model missing, 400 for bad params |
| `GET /v1/recommend` | `Recommend` | Ranked model list; maps `?task=` to modality filter, filters by `?context=` and `?max_price_input=`; post-fetch modality filtering keeps the Store interface simple |
| `GET /v1/context` | `GetContext` | Compact pricing snapshot capped at 2 100 tokens (4-chars/token approx); starts with 50 models and trims one at a time until the full serialised `Envelope` fits |

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
