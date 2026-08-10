# internal/auth

Magic-link signup HTTP handlers for the `/auth` route group.

## Purpose

Implements the self-serve onboarding flow that turns an email address into an Unkey API key, with no password and no dashboard login. It is the HTTP layer only: all persistence, token minting, and key issuance are delegated to [`internal/signup`](../signup/README.md), and email delivery to [`internal/mailer`](../mailer/README.md).

This package is what `cmd/api` mounts, and the only signup HTTP layer that exists — a duplicate stack in `internal/signup` was removed.

Routes are public (no API key) and rate-limited per IP by `middleware.IPRateLimit` in `cmd/api/main.go`.

## Structure

```
internal/auth/
  handlers.go       # Handler type, Config, AbuseGuard iface, Register, all five endpoint methods
  handlers_test.go  # Unit tests with mock Store/Mailer/KeyIssuer/AbuseGuard
  e2e_test.go       # End-to-end flow test across the full handler chain
  README.md         # This file
```

## Key Components

### Collaborator interfaces

The handler depends on four narrow interfaces rather than concrete types, so tests substitute mocks without a database or network:

| Interface | Production implementation | Role |
|---|---|---|
| `Store` | `signup.NewStore(db)` | Identity rows, magic-link tokens, API key registry |
| `Mailer` | `mailer.New(...)` | Sends the verification email |
| `KeyIssuer` | `signup.NewUnkeyIssuer(...)` | Creates and revokes Unkey API keys |
| `AbuseGuard` | `signup.NewAbuseGuard(...)` | Disposable-domain block, per-IP limit, resend + regenerate cooldowns |

### `Config`

Carries the signing secret, magic-link TTL and URL construction inputs, session cookie name/TTL/`Secure` flag, and the `SignupEnabled` kill switch. Populated from `internal/config` in `cmd/api`.

### `Handler`

```go
func New(store Store, mailer Mailer, issuer KeyIssuer, guard AbuseGuard, cfg Config, log zerolog.Logger) *Handler
func Register(router fiber.Router, h *Handler)
```

`guard` may be `nil`, which disables every abuse control. Tests do that; **production must not** —
`request-link` emails an arbitrary address, so without the per-email cooldown it can be used to
mail-bomb a third party.

`Register` mounts five routes:

| Route | Session required | Behaviour |
|---|---|---|
| `POST /signup/request-link` | No | Validates the email, mints a one-time token, emails the link |
| `GET /signup/verify` | No | Consumes the token, sets the signed session cookie |
| `GET /signup/me` | Yes | Returns the verified identity and whether a key already exists |
| `POST /signup/issue-key` | Yes | Issues the account's API key (plaintext returned once) |
| `POST /signup/regenerate-key` | Yes | Revokes the existing key and issues a replacement |

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
}, log)

group := app.Group("/auth", middleware.IPRateLimit(redisClient, log))
auth.Register(group, h)
```

## Design Notes

- **Enumeration-safe responses.** `RequestLink` returns the same 200 body whether or not the address maps to an existing identity, and a mailer failure does not change the response. Failures are logged server-side only.
- **The resend cooldown is deliberately invisible.** It maps to a success-shaped `200` with the send suppressed, not a `429` — the cooldown protects an inbox the caller may not own, and a distinct status would reveal that a link was recently sent there. The per-IP limit (`429`) and disposable-domain block (`400`) describe the caller's own request, so those are surfaced plainly.
- **`SIGNUP_ENABLED=false` is enforced in the handler, not the router.** The IP rate limiter still wraps the group so the routes cannot be used to generate load while signup is off; the handlers return 503.
- **The API key is returned exactly once.** Only a hash is retained in the registry, so a lost key must be replaced via `regenerate-key`.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/signup` | `Store`, `SessionPayload`, token/session crypto, `AbuseGuard`, Unkey issuer |
| `internal/mailer` | Magic-link email delivery |
| `internal/api` | RFC 7807 error responses |
| `internal/middleware` | `IPRateLimit` (applied by the caller) |
| `internal/logger` | zerolog logger construction in tests |
| `github.com/gofiber/fiber/v2` | HTTP routing and context |
| `github.com/rs/zerolog` | Structured logging |
