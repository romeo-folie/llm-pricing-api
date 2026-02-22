# Phase: monetisation
> Implement Lemon Squeezy billing, Unkey key lifecycle, email delivery via Resend, and a self-serve account dashboard.

**Branch:** `epic/monetisation` | **Worktree:** `../epic-monetisation` | **Created:** 2026-02-22

---

## Current Status

| # | Task | Status | GitHub |
|---|------|--------|--------|
| #50 | DB Migrations — billing_subscriptions & webhook_events | done | [#50](https://github.com/romeo-folie/llm-pricing-api/issues/50) |
| #51 | Billing Service (`internal/billing`) | done | [#51](https://github.com/romeo-folie/llm-pricing-api/issues/51) |
| #52 | Lemon Squeezy Webhook Handler | done | [#52](https://github.com/romeo-folie/llm-pricing-api/issues/52) |
| #53 | Free Signup Endpoint (`POST /v1/signup/free`) | done | [#53](https://github.com/romeo-folie/llm-pricing-api/issues/53) |
| #54 | Key Revocation Asynq Job (`billing:revoke-key`) | done | [#54](https://github.com/romeo-folie/llm-pricing-api/issues/54) |
| #55 | Account Dashboard (`GET /v1/account`) | done | [#55](https://github.com/romeo-folie/llm-pricing-api/issues/55) |
| #56 | Free Signup UI (Next.js page) | done | [#56](https://github.com/romeo-folie/llm-pricing-api/issues/56) |
| #57 | E2E Billing Integration Tests | open | [#57](https://github.com/romeo-folie/llm-pricing-api/issues/57) |

**Next action:** Run `/pm:issue-start 57` to begin E2E Billing Integration Tests.

---

## Goals

- Self-serve Free tier signup (no payment needed): `POST /v1/auth/signup` → Unkey key issued → welcome email via Resend
- Paid tier (Developer/Pro) provisioning via Lemon Squeezy webhooks: key creation, tier updates, delayed revocation at `ends_at`
- Delayed key revocation via asynq `billing:revoke-key` job scheduled at `ends_at` (LS field for cancelled subscriptions)
- `subscription_resumed` cancels the pending revocation job (stored in `billing_subscriptions.revoke_job_id`)
- Account dashboard at `GET /v1/account` — plan card, API key display, usage stats, LS customer portal link
- Free signup UI page at `/signup` in Next.js frontend
- E2E tests covering the full billing flow

---

## Architecture Notes

- **`internal/billing/`** — new package; owns `Service` struct with `CreateKey`, `UpdateKeyTier`, `RevokeKey`, `SendKeyDelivery`, `SendPlanChange`. Depends on Unkey SDK + Resend SDK.
- **Unkey** — already integrated for key *verification* (`internal/middleware/auth.go`). New usage: key creation/update/revocation via the Unkey management API. Inject `UnkeyRootKey` from env.
- **Lemon Squeezy webhooks** — registered at `POST /webhooks/lemon-squeezy` on the Fiber app root (outside `/v1/` — no API key auth on this route). HMAC-SHA256 verified via `X-Signature` header and `LEMONSQUEEZY_SIGNING_SECRET`.
- **Critical LS field**: use `ends_at` (not `renews_at`) for cancelled subscription expiry. `renews_at` = next active billing date; `ends_at` = access end date for cancelled subs.
- **`subscription_resumed`**: must cancel the pending asynq `billing:revoke-key` job. Task ID stored in `billing_subscriptions.revoke_job_id`.
- **asynq** — already used for scrapers/webhooks. New task constant `TaskBillingRevokeKey = "billing:revoke-key"`. Handler registered in `cmd/worker/main.go`. Enqueued with `asynq.ProcessAt(ends_at)`.
- **Resend** — email provider (Go SDK). Templates via `html/template`. Sends welcome email (key delivery) and plan change notification.
- **DB tables**: `billing_subscriptions` and `webhook_events` (migration 000009). `billing_subscriptions.revoke_job_id TEXT` is nullable — populated on `subscription_cancelled`, cleared on `subscription_resumed`.
- **Free signup**: `POST /v1/auth/signup` is *unauthenticated* (no API key required). Rate-limited separately. Creates Unkey key at Free tier, stores in DB, sends welcome email.
- **Account dashboard**: `GET /v1/account` requires API key auth (any tier). Returns plan, key metadata, usage from Unkey, LS customer portal URL (24h validity from `data.attributes.urls.customer_portal`).
- **Session / CSRF**: account dashboard Next.js page uses a signed cookie (`SESSION_SECRET`) to store the API key client-side. The same `SESSION_SECRET` must be set in both Railway (API) and Vercel (frontend) environments.
- **Tier mapping**: LS variant/product ID → tier string via env vars `LS_VARIANT_DEV` and `LS_VARIANT_PRO`.

---

## Key Files

| Path | Role |
|------|------|
| `internal/billing/` | New package — billing service (primary work area for #51) |
| `internal/api/handlers/billing.go` | New file — LS webhook handler (#52) and signup endpoint (#53) |
| `internal/worker/billing_handler.go` | New file — asynq `billing:revoke-key` handler (#54) |
| `internal/worker/tasks.go` | Add `TaskBillingRevokeKey` constant (#54) |
| `cmd/worker/main.go` | Register new handler (#54) |
| `cmd/api/main.go` | Register `/webhooks/lemon-squeezy` + `/v1/auth/signup` + `/v1/account` routes |
| `migrations/` | New 000009 migration pair (#50) |
| `frontend/app/signup/` | New signup page + form (#56) |
| `.env.example` | New env vars: `LEMONSQUEEZY_SIGNING_SECRET`, `UNKEY_ROOT_KEY`, `RESEND_API_KEY`, `LS_VARIANT_DEV`, `LS_VARIANT_PRO`, `SESSION_SECRET` |

---

## Dependency Order

```
#50 (DB migrations)
  └── #51 (billing service)
        ├── #52 (webhook handler)   ← parallel once #51 done
        ├── #53 (free signup)       ← parallel once #51 done
        └── #54 (revoke-key job)    ← parallel once #51 done
              └── #55 (account dashboard)
                    └── #56 (signup UI)
                          └── #57 (E2E tests)
```

---

## CCPM Quick Commands

```text
/pm:next                          # What to work on next
/pm:issue-start <N>               # Claim and begin a task
/pm:issue-close <N>               # Mark complete, update this file
/pm:epic-status monetisation      # This epic's progress
/pm:blocked                       # See blocked tasks
```

---

## Resuming Work

1. Check the **Current Status** table above for the next `open` task with no open blockers.
2. Run `/pm:issue-start <N>` to claim it and read its full spec on GitHub.
3. Write tests first (TDD per CLAUDE.md), then implement.
4. Run `golangci-lint run ./...` and `go test ./...` — both must be clean before committing.
5. Run `/code-reviewer` (Sonnet) then `/code-reviewer` (Opus) — fix all findings.
6. Run `/pm:issue-close <N>` to mark complete and update the **Current Status** table above.
7. **Never use `--no-verify`** — the pre-commit hook runs lint + tests automatically.
8. After closing the last task (#57), run the full-epic Opus review before `/pm:epic-close monetisation`.
