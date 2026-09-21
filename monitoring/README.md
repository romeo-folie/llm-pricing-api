# Monitoring

Observability infrastructure-as-code for the LLM Pricing Platform.

## Architecture

Every signal leaves the platform through a single OpenTelemetry Collector:

```
api                ─┐
worker             ─┤  pull /metrics (Prometheus receiver)
postgres-exporter  ─┤──────────────────► prometheusremotewrite ─► Grafana Cloud Mimir
redis-exporter     ─┘

api (OTLP gRPC)    ──── otlp receiver ─► otlphttp ─► Grafana Cloud Tempo
```

The collector runs in the same Railway project as the API and worker so the
`*.railway.internal` scrape targets resolve. There is no direct app→Grafana hop:
the collector owns TLS and credentials, so `internal/otel` keeps its plaintext
gRPC exporter and needs no change when the backend moves.

## Files

| Path | Role |
|---|---|
| `otel-collector.yaml` | Collector config — receivers, processors, exporters, pipelines |
| `dashboards/*.json` | Four Grafana dashboards, provisioned through the Grafana API |
| `alerts/rules.yaml` | Prometheus-format alert rules (source of truth) |
| `provision/` | Provisioning script and its README |

`grafana-agent` was removed: Grafana Agent is on a deprecation path in favour of
Alloy, and the Collector already matches the intended topology.

## Why metrics use remote_write and not OTLP

Deliberate, not incidental. `prometheusremotewrite` preserves the native
Prometheus data model — `up`, `job` and `instance` reach Mimir exactly as the
Prometheus receiver scraped them. The dashboards and alert rules are written
against that model, and `absent(up{job="llm-pricing-api"})` (issue #194) depends
on it. Routing metrics over OTLP instead would map them through resource
attributes and silently stop those expressions matching.

A related trap: the remote_write hostname contains a **shard number**
(`prometheus-prod-65-…`) that is only visible in the datasource settings.
Guessing the shard range returns a Cloudflare 1016, which is easy to misread as
"this stack has no remote_write endpoint".

## Required environment variables

Telemetry ingest — access-policy token (`glc_`):

| Variable | Purpose |
|---|---|
| `GRAFANA_CLOUD_OTLP_ENDPOINT` | OTLP gateway base URL (`/v1/traces`, `/v1/logs`) |
| `GRAFANA_CLOUD_BASIC_AUTH_HEADER` | Full `Basic …` header value for the gateway |
| `GRAFANA_CLOUD_URL` | Prometheus remote_write endpoint |
| `GRAFANA_CLOUD_USER` | Metrics instance ID — not the stack ID |
| `GRAFANA_CLOUD_API_KEY` | Access-policy token with `metrics:write` |

Provisioning — service-account token (`glsa_`):

| Variable | Purpose |
|---|---|
| `GRAFANA_API_URL` | Grafana instance URL |
| `GRAFANA_API_TOKEN` | Service-account token, role Admin |

`glc_` tokens are **rejected** by the Grafana instance API with
`401 Invalid API key`. The two token types are not interchangeable; see
`.env.example` for the full explanation.

## Scrape targets

Supplied per environment so one config file serves both local and production:

| Variable | Local (docker-compose) | Railway |
|---|---|---|
| `API_METRICS_TARGET` | `host.docker.internal:9091` | `llm-pricing-api.railway.internal:9091` |
| `WORKER_METRICS_TARGET` | `host.docker.internal:9092` | `llm-pricing-worker.railway.internal:9091` |
| `POSTGRES_EXPORTER_TARGET` | `postgres-exporter:9187` | `postgres-exporter.railway.internal:9187` |
| `REDIS_EXPORTER_TARGET` | `redis-exporter:9121` | `redis-exporter.railway.internal:9121` |

In production the two exporter targets must exist as Railway services named
exactly `postgres-exporter` and `redis-exporter` (`prometheuscommunity/postgres-exporter:v0.20.1`
and `oliver006/redis_exporter:v1.91.1`); see `DEPLOY.md`. They need no public
domain — the collector reaches them over private networking. In local compose
they are declared alongside the collector.

Both binaries serve `/metrics` on `METRICS_PORT` (default `9091`) via
`metrics.NewServer` in `internal/metrics`. Prometheus scrapes per process, so the
API and worker are scraped separately — the API's endpoint does **not** carry the
worker's pipeline counters (`llm_scraper_runs_total`,
`llm_reconciler_events_total`, `llm_webhook_deliveries_total`), because those
exist only in the worker process. The worker's local port is offset to `9092` by
the `worker` target in the Makefile so both can run at once.

## Local compose

`docker-compose.yml` runs `postgres-exporter` (9187), `redis-exporter` (9121) and
`otel-collector` (OTLP 4317/4318, health 13133). The API and worker run on the
host via `make run` / `make worker`, hence the `host.docker.internal` targets.

## Provisioning

Dashboards, the email contact point and the alert rules are applied by
`provision/provision.py`. See [`provision/README.md`](provision/README.md).
