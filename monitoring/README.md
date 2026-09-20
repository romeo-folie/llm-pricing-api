# Monitoring

Infrastructure-as-code for LLM Pricing observability.

## Files

- `agent.yml` — Grafana Agent scrape + remote_write config
- `dashboards/*.json` — Grafana dashboards (import/provision as code)
- `alerts/rules.yaml` — Prometheus alerting rules

## Required environment variables

- `GRAFANA_CLOUD_URL` — Prometheus remote_write endpoint URL
- `GRAFANA_CLOUD_USER` — Grafana Cloud metrics instance ID / user
- `GRAFANA_CLOUD_API_KEY` — Grafana Cloud API key (metrics:write)

## Local compose

`docker-compose.yml` includes:
- `postgres-exporter` (9187)
- `redis-exporter` (9121)
- `grafana-agent` (scrapes the exporters and both services' `/metrics`, then forwards to Grafana Cloud)

Both binaries serve `/metrics` on `METRICS_PORT` (default `9091`) via `metrics.NewServer` in
`internal/metrics`. Prometheus scrapes per process, so the agent scrapes them separately:

- API — `host.docker.internal:9091` (`cmd/api`)
- Worker — `host.docker.internal:9092` (`cmd/worker`)

The worker's port is offset to `9092` by the `worker` target in the Makefile so it can run alongside
the API locally. Scraping only the API leaves the pipeline counters
(`llm_scraper_runs_total`, `llm_reconciler_events_total`, `llm_webhook_deliveries_total`) empty —
those exist only in the worker process.

> The `grafana-agent` configuration here is local-only. It is replaced by an OpenTelemetry Collector
> in production; see the observability epic.
