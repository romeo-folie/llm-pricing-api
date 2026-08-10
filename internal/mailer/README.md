# internal/mailer

Transactional email delivery via the Resend REST API.

## Purpose

Sends the magic-link verification email for the signup flow. Deliberately has **zero third-party dependencies** — it calls `POST https://api.resend.com/emails` with `net/http` because the API surface needed is one endpoint with a JSON body and a bearer token.

This is the mailer that `cmd/api` wires into [`internal/auth`](../auth/README.md).

## Structure

```
internal/mailer/
  mailer.go       # Mailer type, New, SetBaseURL, SendMagicLink, HTML template
  mailer_test.go  # Tests against an httptest.Server via SetBaseURL
  README.md       # This file
```

## Key Components

### `Mailer`

```go
func New(apiKey, from string) *Mailer
func (m *Mailer) SendMagicLink(ctx context.Context, toEmail, verifyURL string) error
func (m *Mailer) SetBaseURL(u string)  // testing only
```

`New` builds a client with a **10-second HTTP timeout**. `from` is a full RFC 5322 sender (e.g. `"LLMRates <noreply@llmrates.live>"`).

`SendMagicLink` renders the HTML body from an internal template, posts it to Resend, and wraps any transport or non-2xx response in an error prefixed `mailer:`. `verifyURL` must already contain the raw token query parameter — this package does not construct links (see `signup.BuildVerifyURL`).

`SetBaseURL` redirects requests at an `httptest.Server` so tests never reach Resend.

## Usage

```go
ml := mailer.New(cfg.ResendAPIKey, cfg.EmailFrom)

if err := ml.SendMagicLink(ctx, "user@example.com", verifyURL); err != nil {
    // Log only. Never surface mailer failures to the caller — doing so would
    // leak whether an address is registered.
    log.Error().Err(err).Msg("send magic link")
}
```

## Design Notes

- **No SDK.** This is the only Resend client in the codebase (a duplicate SDK-based one in `internal/signup` was removed). Keeping the dependency surface at stdlib means it can be reasoned about and tested in isolation.
- **Context-aware.** `SendMagicLink` builds the request with `http.NewRequestWithContext`, so a cancelled request context aborts the send rather than blocking the handler.
- **The API key never reaches a log line.** It is set on the `Authorization` header only; errors returned from this package describe the HTTP status, never the credential.

## Dependencies

None beyond the standard library (`net/http`, `encoding/json`, `bytes`, `context`, `time`, `fmt`, `io`).

Consumed by [`internal/auth`](../auth/README.md), which depends on it through its own `Mailer` interface.
