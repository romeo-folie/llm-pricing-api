# Magic-Link Signup Rollout Runbook

Production rollout guide for the magic-link free API key issuance flow.

## 1. Pre-Deploy Checklist

Before deploying, verify every item below:

### Secrets & Configuration

- [ ] `MAGIC_LINK_SIGNING_SECRET` is set and stable across restarts (minimum 32 bytes). Generate with: `openssl rand -hex 32`
- [ ] `RESEND_API_KEY` is set and valid (obtain from [resend.com/api-keys](https://resend.com/api-keys))
- [ ] `UNKEY_ROOT_KEY` and `UNKEY_API_ID` are configured (required for key issuance)
- [ ] `EMAIL_FROM` is set (e.g. `LLMRates <noreply@llmrates.live>`)
- [ ] `SIGNUP_ENABLED` is set to `false` for the initial dark-launch deploy

### Database

- [ ] Migration `000012` (api_identities table) has been applied
- [ ] Migration `000013` (magic_link_tokens table) has been applied
- [ ] Verify: `SELECT COUNT(*) FROM api_identities;` returns 0 (no stale test data)

### Redis

- [ ] Redis is reachable from the API service (`REDIS_URL` is correct)
- [ ] Rate limit keys use prefix `iprl:` — confirm no key namespace collision

### DNS / Email (Resend)

See [DNS Verification](#4-dns--resend-verification) below.

## 2. Rollout Sequence

### Phase 1: Dark Launch

Deploy with signup disabled to verify the deployment itself is healthy.

```bash
# Set env vars on Railway (or equivalent)
SIGNUP_ENABLED=false

# Deploy
railway up
```

**Smoke tests:**
- [ ] `GET /health` returns 200
- [ ] `POST /auth/signup/request-link` returns **503** with `{"error":"signup is currently disabled"}`
- [ ] Existing `/v1/` endpoints continue to function normally
- [ ] No new errors in application logs

### Phase 2: Enable Signup

```bash
SIGNUP_ENABLED=true
# Redeploy or restart the service
```

**Verification:**
- [ ] `POST /auth/signup/request-link` with a valid email returns 200
- [ ] Magic-link email arrives within 30 seconds
- [ ] Clicking the link in the email sets a session cookie and returns `{"verified": true}`
- [ ] `GET /auth/signup/me` with the session cookie returns identity data

## 3. Monitoring

### Log Lines to Watch

| Log message | Severity | Meaning |
|---|---|---|
| `auth: magic-link email delivery failed` | WARN | Resend API returned an error — check API key and DNS |
| `auth: upsert identity failed` | ERROR | Database write failure — check DB connectivity |
| `auth: consume token failed` | ERROR | Unexpected token consumption error |
| `auth: sign session failed` | ERROR | HMAC signing failure — check `MAGIC_LINK_SIGNING_SECRET` |
| `ipratelimit: ExpireAt failed` | ERROR | Redis TTL issue — rate limit keys may leak |

### Metrics to Track

- **Error rate** on `POST /auth/signup/request-link` — should be near 0% for 200s
- **Error rate** on `GET /auth/signup/verify` — expect some 410s (expired/reused tokens), 401s should be rare
- **Rate limit hits** (429 responses on `/auth/` routes) — high volume may indicate abuse
- **Email delivery latency** — check Resend dashboard for bounce/complaint rates

### Alerts (Recommended)

- Error rate on `/auth/signup/request-link` > 5% for 5 minutes
- Error rate on `/auth/signup/verify` > 20% for 5 minutes (excluding 410s)
- Rate limit (429) count > 100/min sustained for 10 minutes

## 4. DNS / Resend Verification

Resend requires DNS records on your sending domain to authenticate outbound email. Without these, emails will land in spam or be rejected.

### Required DNS Records

Configure these on your domain's DNS provider (e.g. Cloudflare, Route53):

#### SPF Record

Add or update the existing SPF TXT record on your root domain:

```
Type: TXT
Name: @ (or llmrates.live)
Value: v=spf1 include:resend.com ~all
```

If you already have an SPF record, append `include:resend.com` before the `~all` or `-all` mechanism.

#### DKIM Record

Add the DKIM TXT record from your Resend dashboard:

```
Type: TXT
Name: resend._domainkey (provided by Resend)
Value: (provided by Resend — a long DKIM public key string)
```

Get the exact values from: **Resend Dashboard > Domains > your domain > DNS Records**.

#### DMARC Record (Recommended)

```
Type: TXT
Name: _dmarc
Value: v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@llmrates.live
```

Start with `p=quarantine` and move to `p=reject` once delivery is confirmed stable.

### Verification Steps

1. Go to [resend.com](https://resend.com) > **Domains**
2. Add `llmrates.live` (or your sending domain) if not already added
3. Add the DNS records Resend provides
4. Wait for DNS propagation (usually 5-30 minutes, can take up to 48 hours)
5. Click **Verify** in the Resend dashboard — all records should show green checkmarks
6. Send a test email using the Resend API or dashboard to confirm delivery

### Troubleshooting DNS

- **SPF fails**: Ensure no conflicting SPF records exist (only one SPF record per domain)
- **DKIM fails**: Verify the TXT record name matches exactly (including subdomain prefix)
- **Emails in spam**: Check DMARC alignment; ensure From address domain matches SPF/DKIM domain
- **Propagation delay**: Use `dig TXT llmrates.live` to check if records have propagated

## 5. Rollback

If issues arise after enabling signup:

```bash
# Immediate: disable signup (existing sessions remain valid)
SIGNUP_ENABLED=false
# Redeploy or restart
```

This returns `503` on the request-link endpoint. Existing verified users and active sessions are unaffected.

For a full rollback (revert the code change):

1. Revert to the previous deployment
2. Existing identities and tokens in the database are harmless — no data cleanup needed
3. Rate limit keys in Redis expire automatically (15-minute window)
