# internal/auth

Magic-link signup and agent device-grant HTTP handlers for the `/auth` route group.

## Purpose

Implements the two self-serve onboarding flows that turn an email address into an Unkey API key, with no password and no dashboard login:

1. **Magic link** — the user opens a link in their browser and copies the key out.
2. **Agent device grant** — an agent starts a request, the user approves it in a browser, and the key is delivered straight to the agent. The user never handles the secret.

It is the HTTP layer only: all persistence, token minting, and key issuance are delegated to [`internal/signup`](../signup/README.md), and email delivery to [`internal/mailer`](../mailer/README.md).

This package is what `cmd/api` mounts, and the only signup HTTP layer that exists — a duplicate stack in `internal/signup` was removed.

Routes are public (no API key) and rate-limited per IP by `middleware.IPRateLimit` in `cmd/api/main.go`.

## Structure

```
internal/auth/
  handlers.go         # Handler, Config, collaborator interfaces, Register, magic-link + key-management endpoints
  agent.go            # Agent device-grant endpoints and their helpers
  handlers_test.go    # Unit tests with mock Store/Mailer/KeyIssuer/AbuseGuard
  keymgmt_test.go     # Key listing, revocation, and multi-key regeneration tests
  agent_test.go       # Device-grant flow tests, including compensating-failure paths
  e2e_test.go         # End-to-end flow test across the full handler chain
  README.md           # This file
```

## Key Components

### Collaborator interfaces

The handler depends on four narrow interfaces rather than concrete types, so tests substitute mocks without a database or network:

| Interface | Production implementation | Role |
|---|---|---|
| `Store` | `signup.NewStore(db)` | Identity rows, magic-link tokens, key registry, agent grants |
| `Mailer` | `mailer.New(...)` | Sends the verification email |
| `KeyIssuer` | `signup.NewUnkeyIssuer(...)` | Creates and revokes Unkey API keys |
| `AbuseGuard` | `signup.NewAbuseGuard(...)` | Disposable-domain block, per-IP limits, cooldowns, poll throttle |

### `Config`

Carries the signing secret, magic-link TTL and URL construction inputs, session cookie name/TTL/`Secure` flag, the `SignupEnabled` kill switch, and `AgentGrantTTLMinutes`. Populated from `internal/config` in `cmd/api`. `Config.agentGrantTTL()` applies the default when the TTL is unset.

### `Handler`

```go
func New(store Store, mailer Mailer, issuer KeyIssuer, guard AbuseGuard, cfg Config, log zerolog.Logger) *Handler
func Register(router fiber.Router, h *Handler)       // mount at /auth
func RegisterAgent(router fiber.Router, h *Handler)  // mount at /auth/agent
```

`guard` may be `nil`, which disables every abuse control. Tests do that; **production must not** —
`request-link` emails an arbitrary address, so without the per-email cooldown it can be used to
mail-bomb a third party.

**The two registrations exist to allow different rate limits.** An agent legitimately polls
`POST /agent/token` every few seconds for the whole grant lifetime (~120 requests for a 10-minute
grant). Sharing the signup group's limit of 10 requests per 15 minutes would exhaust the bucket in
under a minute and turn every later poll into a `429`, making the flow impossible to finish.
`cmd/api` mounts `/auth` with the signup limit and `/auth/agent` with a poll-scale one
(`agentIPRateLimitMax`). The precise control remains the per-device-code throttle
(`AbuseGuard.CheckAgentPoll`), which is scoped to one grant rather than to an IP that may be shared
by everyone behind a NAT.

`Register` mounts the magic-link and key-management routes:

| Route | Session required | Behaviour |
|---|---|---|
| `POST /signup/request-link` | No | Validates the email, mints a one-time token, emails the link. Accepts an optional `next` for post-verification return. |
| `GET /signup/verify` | No | Consumes the token, sets the signed session cookie, redirects to `next` when safe |
| `GET /signup/me` | Yes | Returns the identity, `has_active_key`, `key_count`, and `max_keys` |
| `POST /signup/issue-key` | Yes | Issues a key (plaintext returned once), or reports `existing` |
| `POST /signup/regenerate-key` | Yes | Rotates one key; `{key_id}` selects which |
| `GET /signup/keys` | Yes | Lists active keys with labels and provenance. Never returns a secret. |
| `DELETE /signup/keys/:id` | Yes | Revokes one key, locally and upstream |

`RegisterAgent` mounts the device-grant routes:

| Route | Session required | Behaviour |
|---|---|---|
| `POST /device` | No | Starts a device grant; returns `device_code`, `user_code`, and verification URLs |
| `GET /grant` | Yes | Describes the pending grant to the user deciding on it |
| `POST /approve` | Yes | Records the user's approve/deny decision, bound to their identity |
| `POST /token` | No | Agent polling endpoint; delivers the key exactly once |

### The device-grant flow (`agent.go`)

The point of the flow is where the credential lands. The magic-link flow delivers a key to a **browser**, which forces the user to move it by hand into the agent's config — through the clipboard, a terminal, or worse, a chat transcript. Here the key travels **server → agent** over the polling back channel and never passes through the user at all. The user's only action is a decision.

The token endpoint answers `200` with a `status` field for every expected outcome (`pending`, `slow_down`, `issued`, `denied`, `expired`, `redeemed`) and reserves problem+json for genuinely malformed or refused requests. An agent polling every 5 seconds for 10 minutes must not generate ~120 4xx responses that pollute error-rate metrics and trip the API's own alerts.

Key-issuance ordering on redemption, which the tests pin:

1. `RedeemAgentGrant` claims the grant atomically. Exactly one concurrent poller can win, so the plaintext is served once.
2. A cap pre-check avoids burning an Unkey create/revoke pair for a user already at the limit.
3. `KeyIssuer.CreateKey`, then `InsertKeyWithLabel(..., CreatedViaAgent)`.
4. On failure the created Unkey key is revoked **and** the grant claim is reverted, so the agent can retry rather than being stranded with a consumed grant and no key.

`GET /agent/grant` and `POST /agent/approve` require a session, so an unauthenticated caller learns nothing about whether a code exists — code-space probing requires an account.

### `RequireSession` and `SessionFromLocals`

`RequireSession` is a Fiber middleware that verifies the signed session cookie and stores the decoded `signup.SessionPayload` in `c.Locals()`. Handlers read it back via `SessionFromLocals` rather than re-parsing the cookie.

## Usage

```go
signupStore := signup.NewStore(db)
ml := mailer.New(cfg.ResendAPIKey, cfg.EmailFrom)
issuer := signup.NewUnkeyIssuer(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
guard := signup.NewAbuseGuard(redisClient, signup.DefaultAbuseConfig(cfg.MagicLinkSigningSecret), log)

h := auth.New(signupStore, ml, issuer, guard, auth.Config{
    SigningSecret:           cfg.MagicLinkSigningSecret,
    MagicLinkTTLMinutes:     cfg.MagicLinkTTLMinutes,
    MagicLinkBaseURL:        cfg.MagicLinkBaseURL,
    MagicLinkPath:           cfg.MagicLinkPath,
    SignupSessionCookieName: cfg.SignupSessionCookieName,
    SignupSessionTTLHours:   cfg.SignupSessionTTLHours,
    SignupSessionSecure:     cfg.SignupSessionSecure,
    SignupEnabled:           cfg.SignupEnabled,
    AgentGrantTTLMinutes:    cfg.AgentGrantTTLMinutes,
}, log)

group := app.Group("/auth", middleware.IPRateLimit(redisClient, log))
auth.Register(group, h)
```

The frontend reaches these routes through Next.js rewrites (`/auth/signup/*` and `/auth/agent/*` are proxied to the API), which is what keeps the session cookie on the browser's own origin.

## Design Notes

- **Enumeration-safe responses.** `RequestLink` returns the same 200 body whether or not the address maps to an existing identity, and a mailer failure does not change the response. `ApproveGrant` returns one message for unknown, already-decided, and already-redeemed codes, since distinguishing them would confirm which codes exist.
- **The resend cooldown is deliberately invisible.** It maps to a success-shaped `200` with the send suppressed, not a `429` — the cooldown protects an inbox the caller may not own, and a distinct status would reveal that a link was recently sent there. The per-IP limit (`429`) and disposable-domain block (`400`) describe the caller's own request, so those are surfaced plainly.
- **`SIGNUP_ENABLED=false` is enforced in the handler, not the router.** The IP rate limiter still wraps the group so the routes cannot be used to generate load while signup is off; the handlers return 503. It gates the agent flow too.
- **Keys are per-agent, so "the key" is no longer a single thing.** `issue-key` keeps its reveal-once semantics (it reports `existing` rather than re-minting on a page reload). `regenerate-key` with no `key_id` targets the newest **magic-link** key, because that is the key the browser flow owns: rotating an agent's key on the user's behalf would break that agent silently. `{key_id}` (from `GET /signup/keys`) overrides that and can target any key. With every slot held by agent keys there is nothing safe to rotate and no room to mint, so it returns `409` asking for an explicit choice.
- **Revocation reports success only when the key is actually dead.** `/v1` authorises against Unkey, not this registry, so a registry-only revocation leaves a working credential. The registry row is disabled first (idempotent), but if the upstream call then fails the handler answers `502` rather than claiming success, and the store tolerates an already-revoked row precisely so the caller can retry and complete the job. Rotation reports the same failure as a `revocation_warning` field instead of failing, because the user's replacement key was already issued and its plaintext is delivered only once.
- **Code probing is bounded per caller, not per code.** `CheckUserCodeAttempt` limits attempts against one code; that alone cannot bound enumeration, because each guess is a different code with a fresh counter. `CheckCodeProbe` is the per-IP control that actually bounds probing, and it also stops one caller burning a known code's budget to lock the legitimate approver out.
- **The emailed magic link cannot carry an approval code.** `SafeNextPath` strips any `code` parameter. `request-link` is unauthenticated and mails an arbitrary address, so a code in `next` would let an attacker send a victim a genuine llmrates.live email that opens the approval screen for the attacker's grant, defeating the code comparison the screen exists to provide. `/activate` keeps the one-click UX by stashing the code in this browser only (`sessionStorage`) across the sign-in round trip, and asks for the code by hand when there is nothing stashed.
- **The agent's client name is untrusted display text.** It is validated for control, format, separator and surrogate characters in `internal/signup` and must be rendered as text on the approval screen, never as markup.

## Known limitations

Deferred deliberately, with reasons, rather than silently skipped:

- **Upstream/registry reconciliation.** If Unkey creates a key but its response is lost, or the process dies between `CreateKey` and `InsertKeyWithLabel`, a live Unkey key exists with no registry row. Reverting the grant makes the agent's *retry* work, but nothing sweeps the orphans. A proper fix needs a durable reconciliation job comparing the registry against Unkey; that is a worker-side feature, not part of this flow.
- **Grant and token pruning is not scheduled.** `DeleteExpiredAgentGrants` and `DeleteExpiredTokens` exist and are tested, but nothing in `cmd/worker` calls them, so `agent_grants` grows with every request. Wiring an asynq cron task is the fix and is tracked separately.
- **The migration DOWN path leaves surplus Unkey keys live.** It revokes all but the newest active row per identity so the unique index can be recreated; it cannot revoke the others at Unkey. A rollback therefore needs an out-of-band Unkey reconciliation pass. The migration comments say so.
- **Expiry mixes clock sources.** The SQL uses `clock_timestamp()` while the handlers compare `expires_at` against `time.Now()`. Under container clock drift the pre-read can disagree with the database. Impact is limited to which message the user sees; the database remains authoritative.
- **Pre-filled approval links remain phishable in principle.** A code in a URL can always be relayed by an attacker who sends their own link, which is inherent to the RFC 8628 `verification_uri_complete` affordance. What is closed here is the stronger variant: a *genuine* llmrates.live email delivering the code. The remaining defence is the approval screen's code comparison and its "only approve if you started this" warning.
- **There is no key-management UI yet.** `GET /auth/signup/keys` and `DELETE /auth/signup/keys/:id` are implemented and tested, but nothing in the frontend calls them. The browser flow still works in the normal cases (rotate the magic-link key, or mint a new one), but the `409` returned when every slot is held by an agent key tells the user to revoke a key with no screen on which to do it. An account/keys page is the fix and is not part of this change.
- **A failed upstream revocation needs the key id to be retried.** `DELETE /signup/keys/:id` returns `502` and is retriable with the same id, so the caller of that endpoint is fine. But the row is already locally revoked, so it no longer appears in `GET /signup/keys`, and `RegenerateKey`'s `revocation_warning` does not carry the replaced key's id. A retry therefore depends on the caller having kept the id, or on operator action. Recording upstream revocation state on the row would close this.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/signup` | `Store`, `SessionPayload`, token/session crypto, `AbuseGuard`, Unkey issuer, device-grant primitives |
| `internal/mailer` | Magic-link email delivery |
| `internal/api` | RFC 7807 error responses |
| `internal/middleware` | `IPRateLimit`, `RealIP` (applied by the caller) |
| `internal/logger` | zerolog logger construction in tests |
| `github.com/gofiber/fiber/v2` | HTTP routing and context |
| `github.com/rs/zerolog` | Structured logging |
