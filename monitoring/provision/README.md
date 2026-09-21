# Provisioning

Applies the LLM Pricing observability stack to Grafana Cloud as code (issue
#195). The folder, dashboards, contact point, alert rules and notification
policy are all created and kept in sync by a single script, so the stack can be
rebuilt from this repository instead of being clicked together in the UI.

Prometheus-format alert rules in `../alerts/rules.yaml` remain the source of
truth; the dashboards in `../dashboards/*.json` remain the source of truth for
panels.

## Usage

From the repository root:

```bash
python3 monitoring/provision/provision.py
```

The script prints a summary table of what it created, updated or left unchanged,
and exits non-zero with a readable message on any HTTP error. It never issues a
`DELETE` request.

## Required environment variables

Read from the repository-root `.env` (never from the shell environment, so a
stale exported token cannot be used by accident):

| Variable | Purpose |
|---|---|
| `GRAFANA_API_URL` | Grafana instance base URL, e.g. `https://llmrates.grafana.net` |
| `GRAFANA_API_TOKEN` | Service-account token (`glsa_…`, role Admin), sent as `Authorization: Bearer …` |

`.env` is parsed in Python, splitting each line on the **first** `=`. Values may
therefore contain spaces and further `=` signs (for example
`GRAFANA_CLOUD_BASIC_AUTH_HEADER=Basic abc==`). The file is never sourced by a
shell, and no value is ever printed.

Only the `glsa_` service-account token works here. `glc_` access-policy tokens
are rejected by the instance API with `401 Invalid API key`; see
`../README.md` for the token-type split.

## What it creates

Everything lives in a folder titled **`LLM Pricing`** with the stable uid
`llm-pricing`:

| Resource | Name / uid | Notes |
|---|---|---|
| Folder | `llm-pricing` / `LLM Pricing` | container for everything below |
| Dashboards | `llm-api-overview`, `llm-data-pipeline`, `llm-infrastructure`, `llm-usage-abuse` | imported with `overwrite: true` |
| Contact point | `llm-pricing-email` | type `email`, `addresses: romeofolie1@gmail.com` |
| Alert rules | six rules in group `llm-pricing-api-alerts` | one per entry in `../alerts/rules.yaml`, in the `llm-pricing` folder |
| Notification policy | root receiver `llm-pricing-email` | existing routes are preserved verbatim |

### Datasource injection

The dashboard JSON files contain no `datasource` field, so panels would bind to
whatever the instance default happens to be. The script injects

```json
{"type": "prometheus", "uid": "grafanacloud-prom"}
```

as the `datasource` on **every panel** and **every target** that lacks one
recursively (including nested rows), so no panel depends on the instance
default. The `grafanacloud-prom` datasource uid is stable across Grafana Cloud
stacks of this type; `grafanacloud-logs` (Loki) and `grafanacloud-traces`
(Tempo) are available if a future dashboard needs them.

### Alert rule translation

Each Prometheus rule becomes a Grafana-managed alert rule:

* `title` ← `alert`
* Prometheus query (`refId: A`) ← `expr` with the **trailing comparison
  stripped**, using `datasourceUid: grafanacloud-prom`
* threshold expression (`refId: C`, the rule `condition`) ← the comparison
  operator and value, using `datasourceUid: __expr__`, reducer `last`
* `for`, `labels.severity` and `annotations.summary` are preserved
* `noDataState: NoData`, `execErrState: Error`

The query's `relativeTimeRange.from` is derived from the largest `[range]`
selector in the expression (plus a 5-minute buffer, minimum 10 minutes) so
`increase(…[1h])`-style windows have enough lookback.

Comparison mapping: `>` → `gt`, `>=` → `gte`, `<` → `lt`, `<=` → `lte`.
`==` and `!=` have no Grafana threshold evaluator; a rule using them, or a rule
whose trailing comparison cannot be identified, **aborts the run with an error
naming the rule** rather than being guessed at.

## Idempotency contract

The script is create-or-update only and safe to run repeatedly. Matching is by
stable identity, never by position:

| Resource | Matched by | Unchanged when |
|---|---|---|
| Folder | uid `llm-pricing`, then title | title already matches |
| Dashboard | dashboard uid (`llm-api-overview`, …) | stored dashboard equals the source file after ignoring `id`/`version`/`iteration` |
| Contact point | `name` | type, `addresses` and `disableResolveMessage` already match |
| Alert rule | `(folderUID, ruleGroup, title)` | query expr, threshold evaluator, `for`, labels and annotations already match |
| Notification policy | the tree itself | root `receiver` is already the email contact point |

A second run therefore reports `unchanged` for every resource and creates no
duplicates.

Two guardrails prevent collateral damage:

* If folder uid `llm-pricing` already exists with a different title, or a
  dashboard uid already exists in a **different** folder, the script aborts
  instead of renaming or moving a resource it did not create.
* Writes are sent with `X-Disable-Provenance: true` so the resources stay
  editable and re-appliable through this API rather than becoming read-only
  provisioned objects.

Nothing is ever deleted. Contact points, folders, dashboards and notification
routes the script did not create are left untouched; existing notification
policy routes are copied through unchanged.

## Hand-rolled YAML parser limitation

**PyYAML is deliberately not a dependency** (standard library only), so
`rules.yaml` is read by a purpose-built line parser in `provision.py`. It
understands exactly the shape that file uses today and nothing more:

* a top-level `groups:` list containing one `- name:` entry,
* that group's `rules:` list,
* rule entries made of `- alert:`, an `expr:` block scalar (`|`) or inline
  scalar, `for:`, and single-key `labels:` / `annotations:` mappings,
* `severity:` and `summary:` are read by key name wherever they appear inside a
  rule (they are not scoped to their parent mapping).

It does **not** support anchors/aliases, multi-document streams, flow-style
collections (`[a, b]`, `{a: b}`), nested mappings beyond one level, multiple
groups, multi-key labels/annotations, or `#` comments on the same line as a
value. A missing required field or an unreadable structure raises an error
instead of being silently misparsed.

If `rules.yaml` grows beyond this shape, replace `parse_rules_yaml` with a real
YAML library — do not extend the parser ad hoc.

## Verifying manually

```bash
# dashboards in the folder
GET /api/search?type=dash-db

# contact points
GET /api/v1/provisioning/contact-points

# alert rules (titles, group, severity labels, `for` durations)
GET /api/v1/provisioning/alert-rules

# notification policy root receiver
GET /api/v1/provisioning/policies
```
