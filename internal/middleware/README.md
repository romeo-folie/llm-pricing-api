# internal/middleware

Fiber middleware for the LLM Pricing API. This package provides authentication, rate limiting, response caching, and security headers.

## Structure

| File | Role |
| --- | --- |
| `auth.go` | Unkey API-key authentication + tier extraction + Redis caching of verification results |
| `ratelimit.go` | Per-key, per-calendar-day rate limiting backed by Redis INCR |
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
| `RequireTier(minTier)` | `fiber.Handler` | Per-route tier enforcement — returns 403 with `tier_required` field if caller is below `minTier` |
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
including webhook registration, checks the tier. The tier constants remain because the rate limiter
and the Prometheus `tier` label still read them.

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

`RequireTier` failures add a `tier_required` field:

```json
{
  "type": "https://llmrates.live/errors/forbidden",
  "title": "Forbidden",
  "status": 403,
  "detail": "this endpoint requires the developer tier or above",
  "tier_required": "developer"
}
```

### Exempt routes

Register `GET /health` and discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/llms.txt`) **outside** the `/v1` route group so they bypass auth automatically.

---

## Rate Limiting (`ratelimit.go`)

### Overview

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
