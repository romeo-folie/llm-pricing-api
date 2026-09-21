# monitoring/synthetic

External uptime probes for the LLM Pricing API, provisioned as code against
**Grafana Cloud Synthetic Monitoring** (issue #194).

## Purpose

The in-pipeline liveness alerts (`LLMScrapeTargetDown`, `LLMAPITrafficAbsent` in
`../alerts/rules.yaml`) can only see the system from inside: they depend on the
collector, Mimir and the services' own metrics endpoints. A synthetic probe adds
an independent, from-the-outside check of the public API — the layer that would
have caught #183, where the API wedged for four days while Railway kept
reporting the deployment `SUCCESS`.

`/health` returns `503` when Postgres or Redis fail their bounded check and
`200` with `{"db":"ok","redis":"ok","status":"ok"}` when healthy, so the HTTP
status code alone is the assertion.

## Why a separate script and credential

`provision/provision.py` targets the **Grafana instance** API. Synthetic
Monitoring is a different service with its own API and its own access token: the
instance service-account token (`GRAFANA_API_TOKEN`) is rejected with
`403 invalid API token`. Hence `provision_checks.py` here and the separate
`GRAFANA_SM_*` variables in `.env`.

## Checks

| Job | Target | Interval | Probes | Assertion |
|---|---|---|---|---|
| `llm-pricing-api-health` | `https://api.llmrates.live/health` | 5 min | 1 | HTTP 200 |
| `llm-pricing-livefire` | a deliberately 404 path | 5 min | 1 | HTTP 200 (fails by design) |

The worker is **not** probed: it has no public domain, and
`LLMScrapeTargetDown` already reports its reachability from the internal scrape.
Exposing `/health` publicly for no extra signal is not worth it.

## Usage

```bash
# Create or update the health check (idempotent; matched by job name)
python3 monitoring/synthetic/provision_checks.py

# Inspect what exists
python3 monitoring/synthetic/provision_checks.py --list

# Pin a probe location instead of auto-selecting the nearest
python3 monitoring/synthetic/provision_checks.py --probe London
```

| Variable (repository-root `.env`) | Purpose |
|---|---|
| `GRAFANA_SM_API_URL` | Region-specific SM API base, e.g. `https://synthetic-monitoring-api-eu-west-2.grafana.net` — the **HTTP API**, not the `-grpc-` host, which is for private probe agents |
| `GRAFANA_SM_ACCESS_TOKEN` | SM access token (Synthetic Monitoring app → Config → Access tokens) |
| `GRAFANA_SM_STACK_ID` | Hosted-graphs stack id |

## Ordering when setting this up

1. Run `provision_checks.py` to create the check.
2. Confirm `probe_success{job="llm-pricing-api-health"}` is arriving in the
   hosted Prometheus (`grafanacloud-llmrates-prom`, the same datasource the
   dashboards use).
3. **Then** run `provision/provision.py` so the `LLMAPIHealthProbeFailing` rule
   is loaded. Loading it first would make it fire `DatasourceNoData` until the
   probe starts publishing.

## Live-fire verification

```bash
python3 monitoring/synthetic/provision_checks.py --live-fire   # create failing check
# …wait for it to fail and confirm the notification reaches the
#    llm-pricing-email contact point…
python3 monitoring/synthetic/provision_checks.py --delete llm-pricing-livefire
```

The only thing that proves the alerting path works end to end is a notification
actually arriving; the probe writes `probe_success` either way.

## Cost

Billed per 10,000 test executions, where one execution is one probe location per
minute of runtime. A 5-minute HTTP check from one probe is
`1 x 1 x 1 x (43,200 / 5)` = **8,640 executions/month**, i.e. 0.86 of a billing
unit, plus a handful of active series at standard metrics rates. Frequency is
the dominant lever: 60s would be 43,200/month, an hour would be 720/month. The
check editor's built-in calculator shows the exact figures for the plan.

## Related

- `../alerts/rules.yaml` — `LLMScrapeTargetDown`, `LLMAPITrafficAbsent`, `LLMAPIHealthProbeFailing`
- `../README.md` — the three liveness layers and the expected response
- `../provision/README.md` — dashboards and alert rules on the Grafana instance API
