# Deployment Runbook

Step-by-step instructions for deploying llm-pricing-api to Railway once all
prerequisites (particularly Unkey keys) are available.

## Prerequisites

- [ ] Railway CLI installed and authenticated (`railway login`)
- [ ] Unkey root key and API ID available (https://app.unkey.com)
- [ ] `hey` installed (`brew install hey`)
- [ ] `go` 1.24+ installed locally
- [ ] All tests green: `go test ./...`

---

## Steps

### 1. Initialize Railway project

```bash
# From the repository root
railway init

# Link to the existing Railway project if already created in the dashboard
railway link
```

### 2. Add PostgreSQL + TimescaleDB database

In the Railway dashboard:
1. Open the project.
2. Click **New** → **Database** → **PostgreSQL**.
3. Railway automatically sets `DATABASE_URL` in the service environment.
4. Enable the TimescaleDB extension after the database is provisioned:

```sql
-- Run once via Railway's database console or psql
CREATE EXTENSION IF NOT EXISTS timescaledb;
```

### 3. Add Redis

In the Railway dashboard:
1. Click **New** → **Database** → **Redis**.
2. Railway automatically sets `REDIS_URL` in the service environment.

### 4. Set environment variables

Use the Railway CLI to set all required variables.  Replace placeholder values
with real credentials before running.

```bash
# Core
railway variables set APP_ENV=production
railway variables set APP_PORT=8080
railway variables set LOG_LEVEL=info

# Admin credentials — use strong, unique values in production
railway variables set ADMIN_USER=admin
railway variables set ADMIN_PASSWORD=<your-strong-password>

# Unkey API key authentication
railway variables set UNKEY_ROOT_KEY=<your-unkey-root-key>
railway variables set UNKEY_API_ID=<your-unkey-api-id>

# Webhook encryption key — generate with: openssl rand -hex 32
railway variables set WEBHOOK_SECRET_KEY=$(openssl rand -hex 32)

# OpenTelemetry (optional — leave empty for no-op)
# railway variables set OTEL_EXPORTER_OTLP_ENDPOINT=https://your-otlp-endpoint
railway variables set OTEL_SERVICE_NAME=llm-pricing-api
```

DATABASE_URL and REDIS_URL are injected automatically by Railway when you link
the database and Redis services.  Do not set them manually unless you are using
external services.

### 5. Deploy

```bash
# Push the current branch to Railway and trigger a build
railway up

# Alternatively, connect the Railway project to the GitHub repository and
# enable automatic deploys from the main branch via the Railway dashboard.
```

Monitor the build log in the Railway dashboard.  A successful deploy will show:

```
starting api  addr=:8080  env=production
```

### 6. Verify health endpoint

```bash
RAILWAY_URL=$(railway domain)

curl -s "https://${RAILWAY_URL}/health" | jq .
# Expected:
# {
#   "status": "ok",
#   "db":     "ok",
#   "redis":  "ok"
# }
```

If status is `degraded`, check the database and Redis connection strings.

### 7. Obtain a developer-tier (or higher) API key from Unkey

Before running the load test you need a valid API key.
**Important**: the load test sends 10,000 requests. A free-tier key allows only
100 requests/day — using one would cause 429 responses after the first 100
requests and produce meaningless p99 results. Use a developer-tier key (10k/day)
or a pro-tier key (unlimited).

1. Log in to https://app.unkey.com.
2. Navigate to the API created for this project (`UNKEY_API_ID`).
3. Create a new key with the `developer` (or `pro`) tier in the metadata.
4. Copy the key — it will not be shown again.

### 8. Warm cache and run load test

```bash
RAILWAY_URL=$(railway domain)
DEV_KEY=<your-developer-or-pro-tier-api-key>

# Warm the cache with a single request
curl -s -o /dev/null \
  -H "Authorization: Bearer ${DEV_KEY}" \
  "https://${RAILWAY_URL}/v1/models"

# Run load test: 10 000 requests, 100 concurrent workers
hey -n 10000 -c 100 \
  -H "Authorization: Bearer ${DEV_KEY}" \
  "https://${RAILWAY_URL}/v1/models" \
  > loadtest-results.txt

# Print the latency percentiles
grep -A5 "Latency distribution" loadtest-results.txt
```

Target: p99 must be under 200 ms.  If p99 exceeds 200 ms:
- Check Redis cache hit rate (add `?nocache=1` to compare cached vs uncached).
- Review Railway service metrics for CPU / memory pressure.
- Check TimescaleDB query plans for missing indexes.

### 9. Commit load test results

```bash
git add loadtest-results.txt
git commit -m "Issue #19: add load test results (p99 < 200ms)"
git push origin epic/rest-api
```

---

## Rollback

If a deploy causes issues, roll back immediately:

```bash
# Redeploy the previous successful deployment via the Railway dashboard
# Deployments → select previous deployment → Redeploy

# Or revert locally and redeploy
git revert HEAD
railway up
```

---

## Monitoring and Observability

- **Railway Metrics**: CPU, memory, and request counts are visible in the
  Railway dashboard under the service Metrics tab.
- **Application logs**: `railway logs --tail` or the Logs tab in the dashboard.
- **OpenTelemetry**: Set `OTEL_EXPORTER_OTLP_ENDPOINT` to forward traces to
  Grafana Cloud, Datadog, Honeycomb, or any OTLP-compatible backend.
- **Health endpoint**: Poll `GET /health` from an external uptime monitor
  (e.g. Better Uptime, UptimeRobot) for availability alerting.

---

## Environment Variable Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `REDIS_URL` | Yes | `localhost:6379` | Redis connection string |
| `APP_ENV` | No | `development` | Runtime environment (`development`/`staging`/`production`) |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `LOG_LEVEL` | No | `debug` | Minimum log level (`trace`/`debug`/`info`/`warn`/`error`) |
| `ADMIN_USER` | No | `admin` | Basic auth username for `/admin` endpoints |
| `ADMIN_PASSWORD` | Yes (prod) | `changeme` | Basic auth password — must be changed in non-dev environments |
| `UNKEY_ROOT_KEY` | No | — | Unkey root key for API key verification |
| `UNKEY_API_ID` | No | — | Unkey API namespace ID |
| `WEBHOOK_SECRET_KEY` | No | (ephemeral) | 32-byte hex AES-256-GCM key for webhook secret encryption |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OTLP endpoint; leave empty for no-op |
| `OTEL_SERVICE_NAME` | No | `llm-pricing-api` | Service name in traces and logs |
