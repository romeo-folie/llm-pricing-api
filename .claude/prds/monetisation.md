---
name: monetisation
description: End-to-end billing and key lifecycle — Lemon Squeezy checkout, Unkey key issuance, Resend email delivery, and an account dashboard with usage stats.
status: backlog
created: 2026-02-22T07:12:52Z
---

# PRD: Monetisation

## Executive Summary

Implement the full monetisation layer for LLMRates: a Lemon Squeezy-powered subscription checkout that issues Unkey API keys scoped to the correct tier upon purchase, a self-serve free-tier signup flow, transactional key delivery via Resend, and an authenticated `/account` dashboard where users can view their tier, copy their key, monitor daily usage, and access the Lemon Squeezy customer portal for plan changes.

The three tiers are:

| Tier | Price | Rate limit |
|---|---|---|
| Free | $0 | 100 req/day |
| Developer | $9.99/mo | 10,000 req/day |
| Pro | $14.99/mo | Unlimited + webhooks + SLA |

---

## Problem Statement

The API is live and serving data but has no way to onboard paying customers. There is no checkout flow, no automated key issuance, and no account page. Users who want Developer or Pro access cannot purchase — the CTA buttons on the pricing page fall back to `/pricing`. Free-tier users also cannot self-serve a key. This phase closes the gap between "running product" and "revenue-generating product".

**Why now:** Phases 1–4 are complete and the product is production-ready. Every day without a billing system is a day of potential revenue lost. The infrastructure (Unkey, Lemon Squeezy account) is already chosen; this phase wires it up.

---

## User Stories

### Persona A — Developer evaluating the API
> "I want to try the API for free without entering a credit card, and upgrade if I find it useful."

- **US-1**: As a developer, I can visit `/pricing` and click "Get Free Key" to fill in my email and instantly receive a Free-tier Unkey API key, so I can start making API calls within minutes.
- **US-2**: As a developer with a Free key, I can see a clear upgrade CTA in my account dashboard that links to the Lemon Squeezy checkout, so I know how to unlock price history and agent endpoints.

### Persona B — Developer purchasing a paid plan
> "I want to pay for the Developer tier and immediately receive a working API key without manual steps."

- **US-3**: As a buyer, I can click "Get API Key" on the pricing page, complete a Lemon Squeezy checkout for the Developer plan ($9.99/mo), and receive my Unkey API key via email within 2 minutes of payment confirmation.
- **US-4**: As a Developer-tier subscriber, my API key immediately allows requests to price history and agent endpoints (`/v1/context`, `/v1/ask`, `/v1/stream/changes`) with no manual intervention.

### Persona C — Existing subscriber managing their account
> "I want to see my current usage, copy my key, and upgrade or cancel from one place."

- **US-5**: As a subscriber, I can log in at `/account` (authenticated via my API key) and see my current tier, masked API key with a copy button, and how many requests I've used today vs. my daily limit.
- **US-6**: As a subscriber, I can click "Manage Subscription" in the account dashboard and be taken to the Lemon Squeezy customer portal where I can upgrade, downgrade, or cancel.
- **US-7**: As a Pro subscriber who cancels, my API key continues to work until the end of the current billing period, after which it is automatically revoked.

### Persona D — Free-tier user upgrading
> "I already have a Free key and want to upgrade to Developer mid-month."

- **US-8**: As a Free-tier user, after completing the Developer checkout, I receive a new Developer-scoped Unkey key via email. My old Free key continues to work at Free limits until I choose to delete it.

---

## Requirements

### Functional Requirements

#### FR-1: Lemon Squeezy Product Setup
- Create 3 LS products: Developer ($9.99/mo recurring), Pro ($14.99/mo recurring). Free tier does not go through LS.
- Configure LS webhook delivery to `POST /webhooks/lemon-squeezy` on the Go API.
- Store `LEMONSQUEEZY_SIGNING_SECRET` and `LEMONSQUEEZY_API_KEY` as environment variables.

#### FR-2: Webhook Handler (`POST /webhooks/lemon-squeezy`)
The endpoint must handle the following LS event types, in order of priority:

| Event | Action |
|---|---|
| `subscription_created` | Call Unkey to issue a new key with `tier=developer` or `tier=pro` metadata; store `ls_subscription_id → unkey_key_id` mapping; email key to customer via Resend |
| `subscription_updated` | If plan changes (upgrade/downgrade), update Unkey key metadata to new tier; email user confirmation |
| `subscription_cancelled` | Mark key for revocation at `renews_at` date; do NOT revoke immediately |
| `subscription_expired` | Revoke Unkey key immediately |

- All events must be **idempotent** — store processed `event_id` values in a DB table; skip duplicate deliveries.
- Respond `200 OK` within 5 seconds for all events, even if downstream (Unkey, Resend) are slow — use background goroutines for slow operations.
- Reject any payload where HMAC-SHA256 signature verification fails with `403 Forbidden`.

#### FR-3: Scheduled Key Revocation
- A background asynq job checks daily for subscriptions with `cancelled_at` where `renews_at` has passed and revokes the corresponding Unkey key.
- Alternatively: enqueue a delayed asynq job at `subscription_cancelled` time, scheduled to fire at `renews_at`.

#### FR-4: Free Tier Signup (`POST /v1/signup/free`)
- Accepts `{ "email": "user@example.com" }`.
- Validates email format server-side; rejects invalid or disposable domains.
- Rate-limits by IP: max 3 free keys per IP per 24h (Redis counter).
- Creates a Unkey key with `tier=free`, `ratelimit={ type: "fast", limit: 100, refillRate: 100, refillInterval: 86400000 }`.
- Sends key delivery email via Resend.
- Returns `{ "message": "API key sent to your email" }` — never expose the key in the HTTP response.

#### FR-5: Free Tier Signup UI
- A modal or dedicated page at `/signup/free` with a single email input field and a "Send my key" button.
- The Free CTA on `/pricing` opens this flow.
- Client-side email validation before submission.
- Success state: "Check your inbox — your key is on its way."
- Error states: invalid email, rate limit hit (show "Too many requests. Try again tomorrow."), server error (generic).

#### FR-6: Resend Email Integration
- Use the Resend Go SDK for all transactional emails from the backend.
- Use React Email templates (TypeScript) for rendering, compiled to HTML.
- Required templates:
  - **Key delivery** (`key-delivery.tsx`): tier name, masked key (show first 8 chars + `...`), full key in a copy-friendly monospace block, link to `/account`, link to API docs.
  - **Plan change confirmation** (`plan-change.tsx`): old tier → new tier, effective date, new daily limit.
  - **Cancellation confirmation** (`cancellation.tsx`): confirms cancellation, states access continues until `renews_at` date.
- Sender domain: configure SPF and DKIM for the sending domain before launch.
- From address: `keys@llmrates.com` (or equivalent configured domain).

#### FR-7: Account Dashboard (`/account`)
Authentication: user submits their API key in a form; the server verifies it via Unkey and stores the result in a short-lived signed cookie (1h TTL). No OAuth, no passwords.

Dashboard sections:
1. **Plan card**: tier badge (Free / Developer / Pro), price, renewal date (from LS API for paid tiers).
2. **API key card**: masked key (`sk-xxxx...`), "Copy full key" button (reveals key on click, copies to clipboard), "View docs" link.
3. **Usage card**: requests used today / daily limit (from Unkey `getKey` response), progress bar, resets at midnight UTC.
4. **Manage subscription** button: links to Lemon Squeezy customer portal URL (retrieved from LS API using `ls_subscription_id`). For Free-tier users: shows upgrade CTAs to Developer and Pro checkout instead.

Dashboard is server-rendered (Next.js Server Component) with the usage card as a Client Component for real-time refresh (polling every 60s).

#### FR-8: Database Schema Additions
New table `billing_subscriptions`:
```sql
CREATE TABLE billing_subscriptions (
  id               BIGSERIAL PRIMARY KEY,
  ls_subscription_id TEXT NOT NULL UNIQUE,
  ls_customer_email  TEXT NOT NULL,
  tier               TEXT NOT NULL CHECK (tier IN ('free', 'developer', 'pro')),
  unkey_key_id       TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'active',
  renews_at          TIMESTAMPTZ,
  cancelled_at       TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE webhook_events (
  id         BIGSERIAL PRIMARY KEY,
  event_id   TEXT NOT NULL UNIQUE,   -- LS event UUID for idempotency
  event_type TEXT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Non-Functional Requirements

### Security
- HMAC-SHA256 webhook signature verification on every LS webhook request — reject before any processing if invalid.
- API key in the account dashboard is never stored server-side; retrieved from Unkey on demand and masked in the UI. Full key shown only on explicit user action (click to reveal).
- Free-tier signup rate-limited by IP at the Redis layer to prevent key farming.
- Signed session cookie for `/account` authentication — short TTL (1h), `HttpOnly`, `Secure`, `SameSite=Lax`.
- Lemon Squeezy signing secret and API key stored as Railway environment variables — never committed.
- Resend API key stored as environment variable — never committed.

### Reliability
- Webhook handler responds `200 OK` within 5 seconds — all downstream calls (Unkey, Resend) run in background goroutines.
- Unkey key issuance failures are retried via asynq with exponential backoff (3 attempts over 15 minutes). No paying user should wait more than 15 minutes for their key.
- If Resend email delivery fails, the key is still issued — email delivery failure must never block key issuance.
- Idempotent webhook processing prevents duplicate keys on LS retry storms.

### Performance
- `/account` dashboard initial load: <1s (server-rendered, minimal DB reads).
- Free signup endpoint: <300ms p99 (Unkey key creation + Redis rate limit check).

### Scalability
- `billing_subscriptions` and `webhook_events` tables will remain small (hundreds to low thousands of rows at launch). No indexing beyond the unique constraints is required initially.

---

## Email Provider Decision

**Selected: Resend**

Justification over ZeptoMail:
- React Email SDK enables `.tsx` templates — consistent with the Next.js stack and easier to maintain alongside the frontend.
- Official Go SDK (`github.com/resendlabs/resend-go`) for the Fiber backend.
- Free tier (3,000/month) sufficient for launch scale.
- Better DX, TypeScript types, and community ecosystem.
- ZeptoMail is cheaper per email at high volume but lacks React Email support and a Go SDK — wrong trade-off at this stage.

Revisit ZeptoMail if monthly send volume exceeds 50,000 emails.

---

## Success Criteria

| Metric | Target | How measured |
|---|---|---|
| End-to-end checkout → key delivery | ≤2 min from payment to email received | Manual test in LS test mode |
| Key works immediately after delivery | 100% — no propagation delay | Automated smoke test post-issuance |
| Free signup → key delivery | ≤60s | Manual test |
| Webhook signature rejection rate | 100% of tampered payloads rejected | Unit test + manual curl with bad sig |
| Cancellation key revocation accuracy | Key revoked within 1h of `renews_at` | Scheduled job test |
| Account dashboard load time | <1s p99 | Lighthouse + manual |
| Unkey key issuance retry success | ≥99% of failures recovered within 15 min | Asynq retry log |

---

## Out of Scope

- **Key rotation button** — deferred; users can contact support to rotate keys in this phase.
- **In-app plan change UI** — all plan changes go via the Lemon Squeezy customer portal link.
- **Team accounts / shared keys** — single-user accounts only.
- **Invoice history in dashboard** — available directly in the LS customer portal.
- **Webhook signing secrets for user webhooks** — the webhook delivery system exists (Pro tier); signing secret management is a follow-on.
- **Usage analytics beyond daily limit** — no historical usage graphs in this phase.
- **Social login / OAuth** — account dashboard uses API key authentication only.
- **Promo codes / trials** — use LS native discount codes if needed; no custom logic.

---

## Dependencies

### External Services
| Service | Dependency | Setup required |
|---|---|---|
| Lemon Squeezy | Checkout, subscription lifecycle webhooks, customer portal | Create Developer + Pro products; configure webhook URL |
| Unkey | API key issuance, tier enforcement, usage tracking | Existing setup; add key creation + update + revoke calls |
| Resend | Transactional email delivery | Create account, configure sending domain, obtain API key |

### Internal
- `internal/middleware/` — existing Unkey validation middleware used for account dashboard auth
- `internal/worker/` — asynq task registration for key revocation jobs
- `migrations/` — two new migrations for `billing_subscriptions` and `webhook_events`
- `frontend/app/` — new pages: `/account`, `/signup/free`
- `frontend/emails/` — new directory for React Email templates

### Environment Variables Required
```
LEMONSQUEEZY_SIGNING_SECRET=
LEMONSQUEEZY_API_KEY=
LEMONSQUEEZY_STORE_ID=
RESEND_API_KEY=
RESEND_FROM_EMAIL=keys@llmrates.com
```

---

## Constraints & Assumptions

- **Lemon Squeezy account is live** — products for Developer and Pro will be created during implementation.
- **Unkey is already configured** — existing namespace and API key are available.
- **Sending domain** — SPF/DKIM setup for Resend is assumed possible on the domain used; if not, use Resend's shared domain as a fallback (lower deliverability).
- **No SCA/3DS complexity** — Lemon Squeezy as MoR handles all tax and compliance; no custom payment logic needed.
- **Free keys are permanent** — Free-tier keys do not expire unless manually revoked by admin.
- **One active key per email** — a user purchasing Developer while already on Free gets a new Developer key; the Free key remains active and is their responsibility to delete.

---

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| LS webhook delivery unreliable | High | Idempotent handler + store all event IDs; nightly reconciliation job against LS API |
| Key email goes to spam | Medium | SPF/DKIM on sending domain; use Resend's deliverability monitoring |
| Unkey key issuance fails after successful payment | High | Asynq retry queue; alert if key not issued within 5 min of subscription_created event |
| Free key farming via disposable email | Medium | IP rate limiting (3/IP/24h) at Redis layer; disposable email domain blocklist |
| LS customer portal URL stale | Low | Retrieve portal URL fresh from LS API on each account dashboard load; cache 5 min |
