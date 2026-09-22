# internal/middleware

Fiber middleware for the LLM Pricing API. This package provides authentication, rate limiting, response caching, and security headers.

## Structure

| File | Role |
| --- | --- |
| `auth.go` | Unkey API-key authentication + tier extraction + Redis caching of verification results |
| `ratelimit.go` | Per-key, per-calendar-day rate limiting backed by Redis INCR |
| `timeout.go` | Per-request deadline on `c.UserContext()` for bounded dependency work; excludes `/v1/stream/*` |
| `timeout_test.go` | Unit tests for `timeout.go` — deadline installed, streaming routes exempt, context cancelled on expiry |
| `auth_test.go` | Unit tests for `auth.go` — covers all acceptance-criteria cases |
| `ratelimit_test.go` | Unit tests for `ratelimit.go` — covers all tier limits and Redis error paths |
| `cache.go` | Response caching middleware (see Issue #16) |
| `security.go` | Security headers middleware (see Issue #16) |

## Authentication (`auth.go`)

### Overview

`Auth` validates `Authorization: Bearer <key>` on every request. It uses the [Unkey Go SDK](https://github.com/unkeyed/unkey-go) to verify keys and caches results in Redis to avoid repeated API calls.

### Key security rules

- The raw API key is **never** stored in Redis, logs, or error messages.
- The Redis cache key is `unkey:{sha256(raw_key)}` — the SHA-256 hex digest of the key.
- Cache TTL: **30 seconds**.

### Flow

1. Extract `Bearer <token>` from the `Authorization` header. Return RFC 7807 `401` if missing or malformed.
2. Look up `unkey:{sha256(token)}` in Redis.
   - **Cache hit**: use the cached valid/invalid result and tier without calling Unkey.
   - **Cache miss**: call `UnkeyVerifier.VerifyKey`, then cache the result.
3. If invalid, return RFC 7807 `401 Unauthorized`.
4. If valid, store the tier (`free`, `developer`, `pro`) in `c.Locals("tier")` and the hash in `c.Locals("key_hash")`, then call `c.Next()`.

### Exposed symbols

| Symbol | Type | Description |
| --- | --- | --- |
| `Auth(verifier, redis, apiID)` | `fiber.Handler` | Main authentication middleware |
| `NewUnkeyClient(rootKey, apiID)` | `UnkeyVerifier` | Production Unkey client (wraps the official SDK) |
| `UnkeyVerifier` | interface | Testable abstraction over the Unkey `VerifyKey` call |
| `LocalKeyTier` | `string` constant | Key used to read the tier from `c.Locals` |
| `LocalKeyHash` | `string` constant | Key used to read the SHA-256 key hash from `c.Locals` |
| `TierFree`, `TierDeveloper`, `TierPro` | `string` constants | Canonical tier names |

### Tier order

```
free  <  developer  <  pro
```

**There is no tier gating.** `RequireTier` was removed once the API became free — no endpoint,
including webhook registration, checks the tier, and no 403 carries a `tier_required` field. The
tier constants remain because the rate limiter and the Prometheus `tier` label still read them.

### The 401 Bearer challenge

Every `401` from `Auth` sets a `WWW-Authenticate` header alongside the RFC 7807 body:

```
WWW-Authenticate: Bearer realm="llmrates", resource_metadata="https://api.llmrates.live/.well-known/oauth-protected-resource"
```

The header is how a client learns *how* to authenticate rather than only that it failed. The
`resource_metadata` pointer lets an MCP client that implements OAuth discovery fetch that document
on its own and find out that keys come from a device-authorization flow instead of an OAuth server.
The URL is hardcoded to production for the same reason `/llms.txt` is: it describes the public API,
not the current deployment.

### RFC 7807 error format

All errors from this package use `Content-Type: application/problem+json`:

```json
{
  "type": "https://llmrates.live/errors/unauthorized",
  "title": "Unauthorized",
  "status": 401,
  "detail": "..."
}
```

### Exempt routes

Register `GET /health` and discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/.well-known/oauth-protected-resource`, `/llms.txt`) **outside** the `/v1` route group so they bypass auth automatically.

---

## Rate Limiting (`ratelimit.go`)

### IP rate limiting (`ipratelimit.go`)

`IPRateLimit(redis, log, trustedProxies...)` bounds public, unauthenticated routes by client IP over a
fixed 15-minute window, defaulting to **10 requests**. `IPRateLimitWithConfig` accepts an
`IPRateLimitConfig` whose `Max` overrides that, and `TimeNow` overrides the clock for deterministic
tests.

`Max` exists because one limit cannot serve both a human signup form and an agent polling for
approval: a device grant legitimately produces a request every few seconds for its whole lifetime
(~120 for a 10-minute grant), which exhausts a signup-shaped bucket in under a minute.

**`Bucket` and `SkipPrefixes` are what make a nested limit actually apply.** Fiber's `Group`/`Use`
match by path **prefix**, so a limiter mounted at `/auth` also runs for `/auth/agent`; and two
limiters that share a Redis counter increment each other's. A nested group therefore needs (a) the
broader limiter to list the nested prefix in `SkipPrefixes`, and (b) its own `Bucket`. Getting either
wrong silently reduces the nested limit rather than raising an error. `cmd/api` mounts `/auth` with
`SkipPrefixes: ["/auth/agent"]` at the default limit and `/auth/agent` with `Bucket: "agent"` and
`Max: agentIPRateLimitMax`. `TestIPRateLimit_NestedGroupKeepsItsOwnBudget` guards the composition.

Client IP comes from `RealIP`, which honours `X-Forwarded-For` only from configured trusted proxies.
The Fiber app sets `EnableIPValidation: true`, so a proxy-header value that does not parse as an IP
falls back instead of being hashed straight into a fresh Redis bucket — without it, a client able to
influence the header would get a new rate-limit bucket per request.

### Per-key rate limiting

`RateLimit` enforces a per-key, per-calendar-day (UTC) request cap. The counter is stored in Redis using an atomic `INCR` + `EXPIREAT` pattern.

Redis key pattern: `ratelimit:{sha256(raw_key)}:{YYYY-MM-DD}`

The SHA-256 hash is read from `c.Locals("key_hash")`, which is set by the `Auth` middleware — so `RateLimit` **must** be applied after `Auth`.

### Tier limits

**The API is free and daily limits are effectively unlimited.** The counters remain so that
per-key usage is still tracked for abuse analytics.

| Tier | Daily limit |
| --- | --- |
| `free` | 1,000,000 requests (`rateLimitFree`) |
| `developer` | 1,000,000 requests (`rateLimitDeveloper`) |
| `pro` | unlimited (no counter touched) |

`free` and `developer` are the same number — the distinction is currently meaningless. Only `pro`
behaves differently, by skipping the counter entirely.

### Counter design

- Redis `INCR` atomically increments the counter and returns the new value.
- On the first request of the day (`count == 1`), `EXPIREAT` is set to midnight UTC and a fallback `EXPIRE` of 25 hours is applied.
- TTL is set only once (on `count == 1`) to avoid resetting the window.

### Failure mode

If Redis is unavailable (INCR returns an error), the middleware **fails open** — the request is allowed through and the counter is not incremented. This prevents a Redis outage from blocking all API traffic.

### 429 response

When the limit is exceeded:

- HTTP `429 Too Many Requests`
- `Retry-After` header set to seconds until midnight UTC
- `Content-Type: application/problem+json`

```json
{
  "type": "https://llmrates.live/errors/rate-limited",
  "title": "Too Many Requests",
  "status": 429,
  "detail": "daily limit of 1000000 requests exceeded; resets at midnight UTC"
}
```

---

## Request Timeout (`timeout.go`)

### Overview

`RequestTimeout(timeout)` installs a deadline on `c.UserContext()` for every request in the group it
is applied to. Database and Redis work is then released when a request overruns, instead of pinning a
pooled connection indefinitely. This is the fix for the four-day wedge in issue #183: all 20 pooled
connections were checked out, later requests blocked forever on pool acquire, and nothing cancelled
them.

### Why `UserContext` and not `Context`

The deadline only cancels work that is handed a derived context. `c.Context()` is fasthttp's request
context and carries no deadline — in fasthttp v1.51 it is cancelled only on server shutdown — so
handlers and middleware must pass `c.UserContext()` to store and Redis calls. `otelfiber` also
installs the request span on `UserContext`, so the switch additionally restores `trace_id`
correlation in database spans and request logs, which `c.Context()` silently dropped.

### Exempt routes

`/v1/stream/*` is deliberately excluded. An SSE connection is expected to outlive any request
deadline, and cancelling its context would sever a live feed and risk leaking the per-key connection
slot the handler releases on exit.

### Failure surface

A request that hits the deadline returns **503 Service Unavailable** as an RFC 7807 Problem Detail —
`api.ErrorHandler` maps `context.DeadlineExceeded` to `NewServiceUnavailable` rather than letting it
fall through to an opaque 500. An overload is a retryable capacity condition, not a bug report.

### Design notes

- **No goroutine.** The middleware narrows the existing context and lets the handler chain unwind
  normally. A handler that ignores its context is therefore still not interrupted — but every pgx
  pool acquire and query respects it, which is what actually frees the connection.
- **Registered first** in the `/v1`, `/auth` and `/admin` groups so every later stage inherits the
  deadline. All three reach the same database pool, so an unbounded request on any of them can
  exhaust it.
- **`defer cancel()`** runs when the chain unwinds, releasing the timer.

## Cache Middleware

See Issue #16 — `cache.go` will be documented here once merged.

## Security Headers Middleware

See Issue #16 — `security.go` will be documented here once merged.

---

## Dependencies

| Dependency | Purpose |
| --- | --- |
| `github.com/gofiber/fiber/v2` | Fiber HTTP framework |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/unkeyed/unkey-go` | Unkey API key verification SDK |

## Usage

```go
// In cmd/api/main.go
unkeyVerifier := middleware.NewUnkeyClient(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
v1 := app.Group("/v1",
    middleware.Auth(unkeyVerifier, redisClient, cfg.UnkeyAPIID),
    middleware.RateLimit(redisClient),
)

// No per-route tier gating — every /v1 route is reachable with any valid key
v1.Post("/webhooks", webhookHandler)
```

## Configuration

| Env var | Required | Description |
| --- | --- | --- |
| `UNKEY_ROOT_KEY` | Yes (for auth) | Unkey root key for verifying API keys |
| `UNKEY_API_ID` | Yes (for auth) | Unkey API ID that keys belong to |
| `REDIS_URL` | Yes | Redis connection URL (used for both auth cache and rate limit counters) |
