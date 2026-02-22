# internal/billing

Provides clients for the three external billing integrations: Lemon Squeezy (subscription management), Unkey (API key lifecycle), and Resend (transactional email).

## Purpose

All external billing SDK calls are contained in this package. Handlers and asynq jobs import `billing` — no external SDK calls appear outside this package. This boundary keeps the rest of the codebase testable via the exported interfaces.

## Structure

| File | Role |
|------|------|
| `billing.go` | `Config`, `Service`, and the three client interfaces (`SubscriptionManager`, `KeyManager`, `Emailer`) |
| `client.go` | `LemonSqueezyClient` — subscription fetch and customer portal URL |
| `unkey.go` | `UnkeyClient` — key creation, tier update, revocation |
| `email.go` | `EmailClient` — Resend transactional email via embedded HTML templates |
| `templates/` | Go `html/template` files for key delivery, plan change, and cancellation |
| `billing_test.go` | Tests for `Service` construction and interface satisfaction |
| `client_test.go` | `httptest`-backed unit tests for `LemonSqueezyClient` |
| `unkey_test.go` | Constructor test + integration tests (guarded by env vars) |
| `email_test.go` | Template parse test + send-error tests with invalid API key |

## Key Components

- **`Service`** — composes `SubscriptionManager`, `KeyManager`, and `Emailer` interfaces. Constructed via `NewService(cfg Config)`. Handlers access `svc.LS`, `svc.Keys`, and `svc.Email` directly. The struct fields are exported so callers can substitute mock implementations in tests.
- **`LemonSqueezyClient`** — plain `net/http` with a 10s timeout. Makes `GET /v1/subscriptions/:id` and `POST /v1/subscriptions/:id/customer-portal-session` calls against the Lemon Squeezy REST API. Normalises the JSON:API envelope into the exported `Subscription` type.
- **`UnkeyClient`** — wraps `github.com/unkeyed/unkey-go`. Stores `Meta["tier"]` on every key, matching what `internal/middleware/auth.go` reads during verification. The tier is the only metadata this platform requires; `UpdateKeyTier` replaces the entire meta map.
- **`EmailClient`** — uses `github.com/resendlabs/resend-go`. Templates are embedded at compile time via `//go:embed`. `NewEmailClient` returns an error if template parsing fails, so misconfigured templates are caught at startup.

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/unkeyed/unkey-go` | Unkey management API (already in go.mod) |
| `github.com/resendlabs/resend-go` | Resend email API |
| `html/template` + `embed` | Email template rendering (stdlib) |
| `net/http` | Lemon Squeezy REST client (stdlib) |

## Usage

```go
svc, err := billing.NewService(billing.Config{
    LSAPIKey:        cfg.LSAPIKey,
    LSStoreID:       cfg.LSStoreID,
    UnkeyRootKey:    cfg.UnkeyRootKey,
    UnkeyAPIID:      cfg.UnkeyAPIID,
    ResendAPIKey:    cfg.ResendAPIKey,
    ResendFromEmail: cfg.ResendFromEmail,
})
if err != nil {
    log.Fatal().Err(err).Msg("failed to init billing service")
}

// Create a Free-tier key and email it.
keyID, keyValue, err := svc.Keys.CreateKey(email, "free")
if err != nil {
    return err
}
go svc.Email.SendKeyDelivery(email, "free", keyValue) // non-blocking fire-and-forget

// Upgrade a key tier after a subscription change.
if err := svc.Keys.UpdateKeyTier(unkeyKeyID, "developer"); err != nil {
    return err
}

// Get the customer self-service portal URL.
portalURL, err := svc.LS.GetCustomerPortalURL(ctx, lsSubscriptionID)
```

## Testing

Unit tests use `net/http/httptest` to mock the Lemon Squeezy API and call real template parsing for the email client. Unkey integration tests are guarded by `UNKEY_ROOT_KEY` / `UNKEY_API_ID` environment variables and are skipped in CI unless those variables are set.

```bash
go test ./internal/billing/...
```
