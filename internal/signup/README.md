# internal/signup

Data access, token/session crypto, abuse controls, agent device-grant records, and Unkey key issuance for the free API-key onboarding flow.

## Purpose

Everything the signup and agent-onboarding flows need except HTTP routing: email identity rows, one-time magic-link tokens, signed session cookies, abuse limits, the per-agent key registry, and creation/revocation of Unkey API keys.

The HTTP layer lives in [`internal/auth`](../auth/README.md), which composes the pieces exported here. There is exactly one implementation of each concern — an earlier duplicate handler stack, session codec, and Resend client were removed once it was established that nothing mounted them.

## Structure

```
internal/signup/
  store.go                   # Store interface, PgxStore, Identity/MagicLinkToken/KeyRecord, HashToken
  store_test.go
  store_integration_test.go  # //go:build integration
  agent.go                   # AgentGrant, device/user code generation, grant state machine
  agent_test.go              # Pure-function tests: code shape, validation, SafeNextPath
  token.go                   # GenerateRawToken, BuildVerifyURL(WithNext), SafeNextPath, session signing
  token_test.go
  abuse.go                   # AbuseConfig, AbuseGuard, disposable-domain denylist, error sentinels
  hash.go                    # HMAC-SHA256 hashing for IPs, emails, codes used as Redis keys
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

`Store` is an **interface**, so handlers and tests depend on the contract rather than on pgx. `NewStore` returns the `*PgxStore` implementation over the migration-000012 tables (`api_identities`, `magic_link_tokens`, `api_keys_registry`) plus `agent_grants` from migration 000019. Safe for concurrent use; no package-level state. Returns `ErrNotFound` for missing rows so callers branch without inspecting driver errors.

Row types: `Identity`, `MagicLinkToken`, `KeyRecord`, `AgentGrant`.

### The key registry (multiple keys per identity)

Keys are **per-agent**, not per-identity. One identity may hold up to `MaxActiveKeysPerIdentity` (5) active keys at once, so each agent can be revoked and attributed independently.

```go
const MaxActiveKeysPerIdentity = 5
const CreatedViaMagicLink = "magic_link"
const CreatedViaAgent     = "agent"

var ErrActiveKeyLimitReached = errors.New("signup: active key limit reached")

GetActiveKey(ctx, identityID)                         // newest active key, or ErrNotFound
ListActiveKeys(ctx, identityID)                       // []KeyRecord, newest first; empty slice if none
InsertKey(ctx, identityID, providerKeyID)             // magic-link key, no label
InsertKeyWithLabel(ctx, identityID, providerKeyID, label, createdVia)
RevokeKey(ctx, identityID, providerKeyID)             // by Unkey provider key ID
RevokeKeyByID(ctx, identityID, keyID) (providerKeyID string, alreadyRevoked bool, err error)
RevokeAndInsertKey(ctx, identityID, oldPK, newPK)     // atomic rotate, inherits label + provenance
```

`KeyRecord` carries `Label` (the agent's self-reported name, empty for magic-link keys) and `CreatedVia`.

**The cap is enforced under a row lock, on both insert paths.** `InsertKeyWithLabel` and
`RevokeAndInsertKey` each open a transaction and take `SELECT 1 FROM api_identities WHERE id = $1 FOR
UPDATE` before counting active keys. Two concurrent writes for the same identity serialize on that
lock. A plain count-then-insert would let both observe an under-cap count and both succeed; a partial
unique index (the previous design) cannot express "at most 5".

Rotation counts too, and the revoke runs before the count so the replaced key is already excluded.
That matters because the second of two concurrent rotations of the *same* key revokes nothing (the
`UPDATE` matches zero rows, which is deliberately non-fatal) and would otherwise insert unconditionally,
leaving one extra key per racing pair. `TestIntegration_RevokeAndInsertKey_ConcurrentRotationsRespectCap`
is the regression test.

`RevokeAndInsertKey` copies the replaced row's `label` and `created_via` onto the replacement, so
rotating an agent's key does not silently relabel it as a magic-link key.

`RevokeKeyByID` is scoped by `identity_id`, so one identity cannot revoke another's key by guessing a
UUID. It deliberately matches already-revoked rows and reports `alreadyRevoked`, so the caller can
retry an upstream Unkey revocation that previously failed; it maps PostgreSQL `22P02` (a malformed
UUID reaching a `uuid` column) to `ErrNotFound` so a bad identifier is a 4xx rather than a 500.

### The agent device-grant flow (`agent.go`)

Backs the "an agent obtains a key on behalf of its user" flow (RFC 8628 shape). Two codes exist per grant and they are deliberately not interchangeable:

| Code | Entropy | Stored as | Role |
|---|---|---|---|
| `device_code` | 256-bit, base64url | SHA-256 only | The credential that redeems the key. Returned once; must never be logged. |
| `user_code` | 8 chars, Crockford base32 | plaintext | Human-typed. Useless on its own: knowing it lets you approve into *your own* identity, not read someone else's key. |

```go
const AgentGrantTTL      = 10 * time.Minute
const AgentPollInterval  = 5 * time.Second

func NewUserCode() (string, error)
func NewDeviceCode() (string, error)
func NormalizeUserCode(raw string) string   // strips separators, upper-cases, I/L→1, O→0
func ValidUserCode(s string) bool
func FormatUserCode(code string) string     // "ACDF0001" → "ACDF-0001"
func ValidateAgentClientName(name string) error
func ValidateAgentPlatform(platform string) error

CreateAgentGrant(ctx, clientName, platform, expiresAt) (*AgentGrant, rawDeviceCode, error)
GetAgentGrantByDeviceCode(ctx, rawDeviceCode) (*AgentGrant, error)
GetAgentGrantByUserCode(ctx, userCode)        (*AgentGrant, error)
DecideAgentGrant(ctx, userCode, identityID, approve) (*AgentGrant, error)
RedeemAgentGrant(ctx, rawDeviceCode)          (*AgentGrant, error)
RevertAgentGrantRedemption(ctx, grantID)      error
DeleteExpiredAgentGrants(ctx)                 (int64, error)
```

`CreateAgentGrant` generates both codes internally and returns the raw device code. That is the only moment it exists in plaintext — the data layer never accepts a caller-supplied device code, so there is no way to accidentally store one. A user-code collision (`~2^-40` per attempt) is retried up to `agentGrantCodeAttempts` times.

Grant lifecycle: `pending` → `approved` | `denied` (either may be followed by `redeemed`), or `expired`. `DecideAgentGrant` binds `identity_id` **only on approval** — a denial must not record which account was signed in. `RedeemAgentGrant` performs the claim as a guarded `UPDATE … WHERE status = 'approved' … RETURNING`, so concurrent polls produce exactly one winner and the plaintext is served once. `RevertAgentGrantRedemption` is the compensating action when key issuance fails after a claim, so a transient upstream error does not strand the agent with a consumed grant and no key.

`ValidateAgentClientName` and `ValidateAgentPlatform` reject **control characters and their invisible cousins**. `unicode.IsControl` covers C0/C1 only, so the check also rejects category Cf (bidirectional overrides like U+202E and zero-width characters like U+200B), Zl/Zp (line/paragraph separators that can forge a line break in log output) and Cs. Both values are echoed on the user's approval screen and in the agent's own terminal output, where an invisible reordering character is an injection vector rather than a rendering curiosity, and where zero-width characters would let two distinct agent names look identical in a key list.

The column list shared by every grant query is a single `agentGrantColumns` constant, so a new column cannot be added to the struct and forgotten in one of the queries.

### Tokens and sessions (`token.go`)

| Function | Purpose |
|---|---|
| `GenerateRawToken()` | Cryptographically random one-time magic-link token |
| `BuildVerifyURL(baseURL, path, rawToken)` | Assembles the emailed link |
| `BuildVerifyURLWithNext(baseURL, path, rawToken, next)` | As above, carrying a post-verification redirect |
| `SafeNextPath(raw)` | Validates a caller-supplied redirect target |
| `SignSession(secret, SessionPayload)` | Produces the signed cookie value |
| `VerifySession(secret, cookieValue)` | Verifies signature and expiry, returns the payload |

The session cookie is `base64url(JSON).<hmac-sha256>` — *signed, not encrypted*, so it must never carry a secret. It embeds email, issued-at, and expires-at so the auth handler avoids a DB round-trip per authenticated request. Only the raw token is emailed; the database stores `HashToken(raw)` (SHA-256).

`SafeNextPath` accepts only same-origin absolute paths and **strips any `code` parameter**. The agent flow uses it to send a user back to `/activate` after verifying their email, so the value arrives in a public request and becomes the `Location` of a 302 — an unchecked value would be an open redirect. Absolute URLs, protocol-relative URLs (`//evil`), and backslash variants (`/\evil`) are all rejected, as are CR/LF and embedded backslashes anywhere in the string. Stripping `code` is what stops an emailed magic link (from an unauthenticated endpoint that mails arbitrary addresses) from pre-loading someone else's approval screen; the approval page keeps the one-click UX by stashing the code in `sessionStorage` instead.

### `AbuseGuard`

```go
func DefaultAbuseConfig(secret string) AbuseConfig
func NewAbuseGuard(rdb *redis.Client, cfg AbuseConfig, log zerolog.Logger) *AbuseGuard

func (g *AbuseGuard) CheckRequestLink(ctx context.Context, ip, email string) error
func (g *AbuseGuard) CheckRegenerateKey(ctx context.Context, identityID string) error
func (g *AbuseGuard) CheckAgentDevice(ctx context.Context, ip string) error
func (g *AbuseGuard) CheckUserCodeAttempt(ctx context.Context, userCode string) error
func (g *AbuseGuard) CheckAgentPoll(ctx context.Context, rawDeviceCode string) error
```

| Control | Default | Sentinel | Handler response |
|---|---|---|---|
| Disposable-domain denylist | on | `ErrDisposableDomain` | `400` |
| Per-IP request-link limit | 5/hour | `ErrRateLimited` | `429` |
| Per-email resend cooldown | 60s | `ErrResendCooldown` | `200` — send suppressed |
| Per-identity regenerate cooldown | 60s | `ErrRegenerateCooldown` | `429` |
| Per-IP device-grant limit | 5/hour | `ErrRateLimited` | `429` |
| Per-code attempt limit | 10 / 10 min | `ErrUserCodeAttempts` | `429` |
| **Per-IP code-probe limit** | 8 / 10 min | `ErrRateLimited` | `429` |
| Per-device-code poll interval | 5s | `ErrPollTooFast` | `200` `slow_down` |

`CheckUserCodeAttempt` is a brake on one code, **not** an enumeration defence: an attacker probing the code space tries a different code each time, and each one starts a fresh counter. `CheckCodeProbe` is the per-IP control that actually bounds probing. Its default (8) is deliberately **below** `MaxUserCodeAttempts` (10) so that one caller cannot spend a known code's entire budget and lock the legitimate approver out for the window.

The resend cooldown returns a **success-shaped response**. It exists to protect the inbox of an address the caller may not own, and a `429` would reveal that a link was recently sent there. The email is simply not sent.

`CheckAgentPoll` is a throttle, not a refusal: the token endpoint answers `ErrPollTooFast` with `200 {"status":"slow_down"}` so a polling agent does not generate a stream of 4xx responses. `rawDeviceCode` is HMAC-hashed before use as a Redis key for the same reason it is hashed at rest.

Redis counters use an atomic Lua `INCR` + `EXPIRE`, which also re-applies the TTL if a key is found with `TTL == -1` — so a transient Redis failure cannot leave a *permanent* rate limit behind. Every Redis-backed control **fails open**: a cache outage must not block signups. A `nil` Redis client disables them all, leaving only the disposable-domain block.

### `hashValue` (`hash.go`)

IPs, emails, user codes, and device codes are stored as **HMAC**-SHA256, not plain SHA-256, before being used as Redis keys. All are low-entropy or credential-bearing, so an unkeyed digest would be trivially reversible offline; the server secret prevents that.

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
- **`AbuseConfig` is narrow by design** — every field is consumed by `AbuseGuard`. It replaced a broad `HandlerConfig` whose remaining fields existed only for the deleted handler stack.
- **Abuse controls carry defaults in code, not env vars.** `DefaultAbuseConfig` is the single place to change them; only the signing secret is injected. `AgentPollInterval` is referenced from the same constant the API advertises as `interval`, so the throttle and the advertised value cannot drift apart.
- **The registry cap is a lock, not an index.** See above; a read-then-write count races and a unique index cannot express a numeric cap.
- **Denials carry no identity.** A `denied` grant has `identity_id = NULL`, enforced by a `CHECK` constraint, so a refusal cannot leak which account was signed in.
- **`Codec` indirection.** JSON marshalling goes through a `Codec` interface rather than package-level function variables, so parallel tests can substitute behaviour without a data race.
- **Signup can be disabled** via `SIGNUP_ENABLED=false`; that check lives in the handler layer, not here. It gates the agent flow too, since both mint keys into the same registry.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/api` | RFC 7807 error helpers (referenced by the HTTP layer, not this package) |
| `github.com/jackc/pgx/v5`, `/pgconn`, `/pgxpool` | Postgres access; `pgconn` for named-constraint detection |
| `github.com/redis/go-redis/v9` | Abuse-guard counters (Lua script) |
| `github.com/rs/zerolog` | Structured logging |

Schema: `migrations/000012_create_identity_tables.up.sql`, `migrations/000019_multi_agent_keys.up.sql`.
