# internal/signup

Data access, token/session crypto, abuse controls, and Unkey key issuance for the free API-key onboarding flow.

## Purpose

Everything the signup flow needs except HTTP routing: email identity rows, one-time magic-link tokens, signed session cookies, abuse limits, and creation/revocation of Unkey API keys.

The HTTP layer lives in [`internal/auth`](../auth/README.md), which composes the pieces exported here. There is exactly one implementation of each concern — an earlier duplicate handler stack, session codec, and Resend client were removed once it was established that nothing mounted them.

## Structure

```
internal/signup/
  store.go                   # Store interface, PgxStore, Identity/MagicLinkToken/KeyRecord, HashToken
  store_test.go
  store_integration_test.go
  token.go                   # GenerateRawToken, BuildVerifyURL, SessionPayload, SignSession, VerifySession
  token_test.go
  abuse.go                   # AbuseConfig, AbuseGuard, disposable-domain denylist, error sentinels
  hash.go                    # HMAC-SHA256 hashing for IPs and emails used as Redis keys
  unkey.go                   # KeyIssuer — Unkey key create/revoke
  json.go                    # Codec indirection to avoid races in parallel tests
  utils.go                   # normalizeEmail
  README.md                  # This file
```

## Key Components

### `Store`

```go
func NewStore(db *pgxpool.Pool) Store   // returns the Store interface, backed by *PgxStore
func HashToken(raw string) string
```

`Store` is an **interface**, so handlers and tests depend on the contract rather than on pgx. `NewStore` returns the `*PgxStore` implementation over the three migration-000012 tables: `api_identities`, `magic_link_tokens`, `api_keys_registry`. Safe for concurrent use; no package-level state. Returns `ErrNotFound` for missing rows so callers branch without inspecting driver errors, and detects the duplicate-active-key constraint violation internally.

Row types: `Identity`, `MagicLinkToken`, `KeyRecord`.

### Tokens and sessions (`token.go`)

| Function | Purpose |
|---|---|
| `GenerateRawToken()` | Cryptographically random one-time magic-link token |
| `BuildVerifyURL(baseURL, path, rawToken)` | Assembles the emailed link |
| `SignSession(secret, SessionPayload)` | Produces the signed cookie value |
| `VerifySession(secret, cookieValue)` | Verifies signature and expiry, returns the payload |

The session cookie is `base64url(JSON).<hmac-sha256>` — *signed, not encrypted*, so it must never carry a secret. It embeds email, issued-at, and expires-at so the auth handler avoids a DB round-trip per authenticated request. Only the raw token is emailed; the database stores `HashToken(raw)` (SHA-256).

### `AbuseGuard`

```go
func DefaultAbuseConfig(secret string) AbuseConfig
func NewAbuseGuard(rdb *redis.Client, cfg AbuseConfig, log zerolog.Logger) *AbuseGuard

func (g *AbuseGuard) CheckRequestLink(ctx context.Context, ip, email string) error
func (g *AbuseGuard) CheckRegenerateKey(ctx context.Context, identityID string) error
```

Four controls, all wired into `internal/auth`:

| Control | Default | Sentinel | Handler response |
|---|---|---|---|
| Disposable-domain denylist | on | `ErrDisposableDomain` | `400` |
| Per-IP request-link limit | 5/hour | `ErrRateLimited` | `429` |
| Per-email resend cooldown | 60s | `ErrResendCooldown` | `200` — send suppressed |
| Per-identity regenerate cooldown | 60s | `ErrRegenerateCooldown` | `429` |

The resend cooldown returns a **success-shaped response**. It exists to protect the inbox of an address the caller may not own, and a `429` would reveal that a link was recently sent there. The email is simply not sent.

Redis counters use an atomic Lua `INCR` + `EXPIRE`, which also re-applies the TTL if a key is found with `TTL == -1` — so a transient Redis failure cannot leave a *permanent* rate limit behind. Every Redis-backed control **fails open**: a cache outage must not block signups. A `nil` Redis client disables them all, leaving only the disposable-domain block.

### `hashValue` (`hash.go`)

IPs and emails are stored as **HMAC**-SHA256, not plain SHA-256, before being used as Redis keys. Both are low-entropy and enumerable, so an unkeyed digest would be trivially reversible offline; the server secret prevents that.

### `KeyIssuer` (`unkey.go`)

```go
func NewUnkeyIssuer(rootKey, apiID string) KeyIssuer
```

Creates and revokes keys through Unkey. `CreateKey` returns the plaintext key and its ID; the plaintext is shown to the user exactly once and never persisted — only the hash goes into `api_keys_registry`.

## Usage

As wired in `cmd/api`:

```go
store  := signup.NewStore(db)
issuer := signup.NewUnkeyIssuer(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
guard  := signup.NewAbuseGuard(
    redisClient,
    signup.DefaultAbuseConfig(cfg.MagicLinkSigningSecret),
    log,
)

// HTTP layer comes from internal/auth
h := auth.New(store, mailer.New(...), issuer, guard, auth.Config{...}, log)
```

Verifying a session outside the middleware:

```go
payload, err := signup.VerifySession(secret, c.Cookies(cookieName))
if err != nil {
    return api.NewUnauthorized("invalid session")
}
```

## Design Notes

- **One implementation per concern.** This package previously carried a second HTTP handler stack (`handlers.go`), two extra token/session codecs (`tokens.go`, `session.go`), a `HandlerConfig` loader, and a Resend client that duplicated [`internal/mailer`](../mailer/README.md). None were mounted by any binary. They were removed rather than documented, so there is no longer a "which one is live?" question.
- **`AbuseConfig` is narrow by design** — five fields, all consumed by `AbuseGuard`. It replaced a broad `HandlerConfig` whose remaining fields existed only for the deleted handler stack.
- **Abuse controls carry defaults in code, not env vars.** `DefaultAbuseConfig` is the single place to change them; only the signing secret is injected.
- **`Codec` indirection.** JSON marshalling goes through a `Codec` interface rather than package-level function variables, so parallel tests can substitute behaviour without a data race.
- **Signup can be disabled** via `SIGNUP_ENABLED=false`; that check lives in the handler layer, not here.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/api` | RFC 7807 error helpers |
| `github.com/jackc/pgx/v5`, `/pgconn`, `/pgxpool` | Postgres access and constraint-violation detection |
| `github.com/redis/go-redis/v9` | Abuse-guard counters (Lua script) |
| `github.com/rs/zerolog` | Structured logging |

Schema: `migrations/000012_create_identity_tables.up.sql`.
