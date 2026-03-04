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
- `grafana-agent` (scrapes exporters + API `/metrics` and forwards to Grafana Cloud)

The API metrics endpoint is served on `METRICS_PORT` (default `9091`) by `cmd/api/main.go`.
For Docker Desktop, the agent scrapes `host.docker.internal:9091`.
