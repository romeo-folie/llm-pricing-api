#!/usr/bin/env python3
"""Provision the LLM Pricing observability stack into Grafana Cloud.

Issue #195. This script applies, as code and idempotently:

  * one folder (``LLM Pricing``, stable uid ``llm-pricing``),
  * the four dashboards in ``monitoring/dashboards/*.json``, with the
    Prometheus datasource injected on every panel and every target,
  * one email contact point (``llm-pricing-email``),
  * the six Grafana-managed alert rules derived from
    ``monitoring/alerts/rules.yaml``, and
  * a notification policy whose root receiver is that contact point.

Hard rules baked into this script:

  * Create/update only. No DELETE request is ever issued, and resources this
    script did not create are never modified or removed.
  * Secrets are never printed. Only URLs, uids, names and HTTP statuses reach
    stdout/stderr.

Standard library only. Safe to run repeatedly.
"""

from __future__ import annotations

import json
import os
import re
import sys
import urllib.error
import urllib.request

# --------------------------------------------------------------------------- #
# Constants
# --------------------------------------------------------------------------- #

REPO_ROOT = os.path.dirname(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
)
ENV_PATH = os.path.join(REPO_ROOT, ".env")
DASHBOARD_DIR = os.path.join(REPO_ROOT, "monitoring", "dashboards")
RULES_PATH = os.path.join(REPO_ROOT, "monitoring", "alerts", "rules.yaml")

FOLDER_TITLE = "LLM Pricing"
FOLDER_UID = "llm-pricing"

PROM_DATASOURCE_UID = "grafanacloud-prom"
PROM_DATASOURCE = {"type": "prometheus", "uid": PROM_DATASOURCE_UID}
EXPR_DATASOURCE_UID = "__expr__"

CONTACT_POINT_NAME = "llm-pricing-email"
CONTACT_POINT_EMAIL = "romeofolie1@gmail.com"

RULE_GROUP = "llm-pricing-api-alerts"
CONDITION_REF_ID = "C"
QUERY_REF_ID = "A"

HTTP_TIMEOUT = 30
DASHBOARD_MESSAGE = "provisioned by monitoring/provision/provision.py (issue #195)"

# Grafana threshold evaluators. `eq`/`neq` are deliberately absent: the
# threshold expression node does not accept them, so such rules are rejected
# rather than guessed at.
COMPARISON_TO_EVALUATOR = {
    ">": "gt",
    ">=": "gte",
    "<": "lt",
    "<=": "lte",
}
UNSUPPORTED_COMPARISONS = {"==": "eq", "!=": "neq"}

DURATION_UNITS = {
    "s": 1,
    "m": 60,
    "h": 3600,
    "d": 86400,
    "w": 604800,
}

# Keys Grafana adds to a stored dashboard that are not part of the source file.
VOLATILE_DASHBOARD_KEYS = ("id", "version", "iteration")


# --------------------------------------------------------------------------- #
# Errors
# --------------------------------------------------------------------------- #


class ProvisionError(RuntimeError):
    """A problem we can explain to the operator without leaking secrets."""


class GrafanaHttpError(ProvisionError):
    def __init__(self, method, path, status, detail):
        self.method = method
        self.path = path
        self.status = status
        self.detail = detail
        status_text = str(status) if status is not None else "no response"
        super().__init__(
            f"HTTP {status_text} on {method} {path}: {detail[:600]}"
        )


class NotFound(GrafanaHttpError):
    pass


# --------------------------------------------------------------------------- #
# .env parsing
# --------------------------------------------------------------------------- #


def read_env(path):
    """Read a KEY=VALUE file into a dict.

    Values are left verbatim except for surrounding whitespace, so values that
    contain spaces or ``=`` (for example ``GRAFANA_CLOUD_BASIC_AUTH_HEADER``)
    survive intact. Surrounding matching quotes are stripped; nothing is
    shell-expanded, interpolated or logged.
    """
    if not os.path.exists(path):
        raise ProvisionError(f"{path} not found")

    env = {}
    with open(path, "r", encoding="utf-8") as handle:
        for raw in handle:
            line = raw.rstrip("\n")
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            if "=" not in line:
                continue
            key, value = line.split("=", 1)
            key = key.strip()
            if key.startswith("export "):
                key = key[len("export ") :].strip()
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
                value = value[1:-1]
            env[key] = value
    return env


# --------------------------------------------------------------------------- #
# HTTP
# --------------------------------------------------------------------------- #


class GrafanaClient:
    def __init__(self, base_url, token):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def call(self, method, path, body=None, extra_headers=None):
        url = self.base_url + path
        data = json.dumps(body).encode("utf-8") if body is not None else None
        request = urllib.request.Request(url, data=data, method=method)
        request.add_header("Authorization", "Bearer " + self.token)
        request.add_header("Accept", "application/json")
        if data is not None:
            request.add_header("Content-Type", "application/json")
        for header, value in (extra_headers or {}).items():
            request.add_header(header, value)

        try:
            with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT) as response:
                status = response.status
                raw = response.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", "replace")
            error_cls = NotFound if exc.code == 404 else GrafanaHttpError
            raise error_cls(method, path, exc.code, raw) from None
        except urllib.error.URLError as exc:
            raise GrafanaHttpError(
                method, path, None, f"connection failed: {exc.reason}"
            ) from None

        if not raw.strip():
            return status, None
        try:
            return status, json.loads(raw)
        except json.JSONDecodeError:
            return status, raw

    def get(self, path):
        return self.call("GET", path)

    def get_optional(self, path):
        """Return the decoded body, or ``None`` when the resource is absent."""
        try:
            return self.call("GET", path)[1]
        except NotFound:
            return None


WRITE_HEADERS = {"X-Disable-Provenance": "true"}


# --------------------------------------------------------------------------- #
# rules.yaml parser (purpose-built, see README)
# --------------------------------------------------------------------------- #

_ALERT_RE = re.compile(r"^(\s*)-\s+alert:\s*(.+?)\s*$")
_GROUP_NAME_RE = re.compile(r"^(\s*)-\s+name:\s*(.+?)\s*$")
_EXPR_BLOCK_RE = re.compile(r"^(\s*)expr:\s*\|-?\s*$")
_EXPR_INLINE_RE = re.compile(r"^(\s*)expr:\s*(.+?)\s*$")
_FOR_RE = re.compile(r"^(\s*)for:\s*(.+?)\s*$")
_SEVERITY_RE = re.compile(r"^(\s*)severity:\s*(.+?)\s*$")
_SUMMARY_RE = re.compile(r"^(\s*)summary:\s*(.+?)\s*$")


def _unquote(value):
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
        return value[1:-1]
    return value


def _dedent(lines):
    indents = [
        len(line) - len(line.lstrip())
        for line in lines
        if line.strip()
    ]
    common = min(indents) if indents else 0
    return "\n".join(line[common:] if line.strip() else "" for line in lines)


def parse_rules_yaml(text):
    """Parse the specific shape used by monitoring/alerts/rules.yaml.

    Returns ``(group_name, rules)`` where each rule is a dict with keys
    ``alert``, ``expr``, ``for``, ``severity`` and ``summary``.

    This is not a general YAML parser. It understands only: a top-level
    ``groups:`` list, one ``- name:`` group entry, a ``rules:`` list, and rule
    entries built from ``- alert:``, an ``expr:`` block or inline scalar,
    ``for:``, and single-key ``labels:`` / ``annotations:`` mappings. Anything
    outside that shape raises ProvisionError instead of being silently
    misread.
    """
    group_name = None
    rules = []
    current = None
    lines = text.splitlines()
    index = 0

    while index < len(lines):
        line = lines[index]

        match = _GROUP_NAME_RE.match(line)
        if match and group_name is None and current is None:
            group_name = _unquote(match.group(2))
            index += 1
            continue

        match = _ALERT_RE.match(line)
        if match:
            current = {
                "alert": _unquote(match.group(2)),
                "expr": None,
                "for": None,
                "severity": None,
                "summary": None,
            }
            rules.append(current)
            index += 1
            continue

        if current is not None:
            match = _EXPR_BLOCK_RE.match(line)
            if match:
                key_indent = len(match.group(1))
                block = []
                index += 1
                while index < len(lines):
                    candidate = lines[index]
                    if not candidate.strip():
                        block.append("")
                        index += 1
                        continue
                    if len(candidate) - len(candidate.lstrip()) <= key_indent:
                        break
                    block.append(candidate)
                    index += 1
                current["expr"] = " ".join(
                    _dedent(block).split()
                ).strip()
                continue

            match = _EXPR_INLINE_RE.match(line)
            if match:
                current["expr"] = _unquote(match.group(2))
                index += 1
                continue

            match = _FOR_RE.match(line)
            if match:
                current["for"] = _unquote(match.group(2))
                index += 1
                continue

            match = _SEVERITY_RE.match(line)
            if match:
                current["severity"] = _unquote(match.group(2))
                index += 1
                continue

            match = _SUMMARY_RE.match(line)
            if match:
                current["summary"] = _unquote(match.group(2))
                index += 1
                continue

        index += 1

    if group_name is None:
        raise ProvisionError(
            f"{RULES_PATH}: could not find a group 'name' entry"
        )
    if not rules:
        raise ProvisionError(f"{RULES_PATH}: no alert rules found")

    for rule in rules:
        missing = [
            key
            for key in ("expr", "for", "severity", "summary")
            if not rule.get(key)
        ]
        if missing:
            raise ProvisionError(
                f"{RULES_PATH}: rule '{rule['alert']}' is missing "
                f"{', '.join(missing)}; refusing to guess"
            )
    return group_name, rules


# --------------------------------------------------------------------------- #
# Duration + PromQL helpers
# --------------------------------------------------------------------------- #


def parse_duration(value):
    """Return seconds for a Prometheus/Grafana duration such as ``5m`` or ``1h30m``."""
    if value is None:
        return None
    text = str(value).strip()
    if not text:
        return None
    if re.fullmatch(r"\d+", text):
        return int(text)
    total = 0
    consumed = 0
    for match in re.finditer(r"(\d+(?:\.\d+)?)([smhdw])", text):
        if match.start() != consumed:
            raise ProvisionError(f"unparseable duration: {value!r}")
        total += float(match.group(1)) * DURATION_UNITS[match.group(2)]
        consumed = match.end()
    if consumed != len(text):
        raise ProvisionError(f"unparseable duration: {value!r}")
    return total


def max_range_seconds(expr):
    """Largest ``[<duration>]`` selector in a PromQL expression, in seconds."""
    longest = 0.0
    for match in re.finditer(r"\[(\d+(?:\.\d+)?[smhdw])\]", expr):
        longest = max(longest, parse_duration(match.group(1)) or 0.0)
    return longest


_TRAILING_COMPARISON_RE = re.compile(
    r"^(?P<query>.+?)\s*(?P<op>>=|<=|==|!=|>|<)\s*"
    r"(?P<value>[-+]?(?:\d+\.\d*|\.\d+|\d+)(?:[eE][-+]?\d+)?)\s*$",
    re.DOTALL,
)


def split_comparison(expr):
    """Split a trailing top-level comparison off a PromQL expression.

    Returns ``(query, evaluator_type, threshold_value)``. Raises ProvisionError
    when no trailing comparison can be identified, or when the comparison has
    no equivalent Grafana threshold evaluator — the task is to stop and report,
    not to guess.
    """
    normalised = " ".join(expr.split())
    match = _TRAILING_COMPARISON_RE.match(normalised)
    if not match:
        raise ProvisionError(
            f"cannot find a trailing comparison in expression: {expr!r}"
        )
    query = match.group("query").strip()
    op = match.group("op")
    value = float(match.group("value"))
    if not query:
        raise ProvisionError(f"empty query before comparison in: {expr!r}")
    if op in UNSUPPORTED_COMPARISONS:
        raise ProvisionError(
            f"comparison {op!r} in {expr!r} has no Grafana threshold evaluator "
            f"(would be {UNSUPPORTED_COMPARISONS[op]!r}); refusing to guess"
        )
    return query, COMPARISON_TO_EVALUATOR[op], value


# --------------------------------------------------------------------------- #
# Resource builders
# --------------------------------------------------------------------------- #


def inject_datasource(node, datasource):
    """Set ``datasource`` on every panel and target that lacks one.

    Returns ``(panels_injected, targets_injected)``.
    """
    panels = 0
    targets = 0
    if isinstance(node, dict):
        if isinstance(node.get("targets"), list):
            if not node.get("datasource"):
                node["datasource"] = dict(datasource)
                panels += 1
            for target in node["targets"]:
                if isinstance(target, dict) and not target.get("datasource"):
                    target["datasource"] = dict(datasource)
                    targets += 1
        for value in node.values():
            sub_panels, sub_targets = inject_datasource(value, datasource)
            panels += sub_panels
            targets += sub_targets
    elif isinstance(node, list):
        for item in node:
            sub_panels, sub_targets = inject_datasource(item, datasource)
            panels += sub_panels
            targets += sub_targets
    return panels, targets


def strip_volatile(dashboard):
    cleaned = dict(dashboard)
    for key in VOLATILE_DASHBOARD_KEYS:
        cleaned.pop(key, None)
    return cleaned


def alert_rule_uid(alert_name):
    """Stable, deterministic uid derived from the Prometheus alert name."""
    return alert_name


def build_alert_rule(rule, folder_uid, org_id, group_name):
    query, evaluator_type, threshold = split_comparison(rule["expr"])
    relative_from = max(600, int(max_range_seconds(rule["expr"]) + 300))
    relative_time_range = {"from": relative_from, "to": 0}

    data = [
        {
            "refId": QUERY_REF_ID,
            "relativeTimeRange": relative_time_range,
            "datasourceUid": PROM_DATASOURCE_UID,
            "model": {
                "editorMode": "code",
                "expr": query,
                "instant": True,
                "range": False,
                "intervalMs": 1000,
                "maxDataPoints": 43200,
                "refId": QUERY_REF_ID,
                "datasource": dict(PROM_DATASOURCE),
            },
        },
        {
            "refId": CONDITION_REF_ID,
            "relativeTimeRange": relative_time_range,
            "datasourceUid": EXPR_DATASOURCE_UID,
            "model": {
                "conditions": [
                    {
                        "evaluator": {"type": evaluator_type, "params": [threshold]},
                        "operator": {"type": "and"},
                        "query": {"params": [CONDITION_REF_ID]},
                        "reducer": {"type": "last", "params": []},
                        "type": "query",
                    }
                ],
                "datasource": {
                    "type": EXPR_DATASOURCE_UID,
                    "uid": EXPR_DATASOURCE_UID,
                },
                "expression": QUERY_REF_ID,
                "refId": CONDITION_REF_ID,
                "type": "threshold",
            },
        },
    ]

    return {
        "uid": alert_rule_uid(rule["alert"]),
        "title": rule["alert"],
        "ruleGroup": group_name,
        "folderUID": folder_uid,
        "orgID": org_id,
        "condition": CONDITION_REF_ID,
        "data": data,
        "for": rule["for"],
        "noDataState": "NoData",
        "execErrState": "Error",
        "labels": {"severity": rule["severity"]},
        "annotations": {"summary": rule["summary"]},
    }


def rule_fingerprint(rule):
    """Semantic projection of an alert rule, for idempotency comparisons."""
    data = []
    for entry in rule.get("data") or []:
        if not isinstance(entry, dict):
            continue
        model = entry.get("model") or {}
        data.append(
            {
                "refId": entry.get("refId"),
                "datasourceUid": entry.get("datasourceUid"),
                "expr": model.get("expr"),
                "expression": model.get("expression"),
                "type": model.get("type"),
                "conditions": model.get("conditions"),
            }
        )
    data.sort(key=lambda item: str(item.get("refId")))
    return {
        "title": rule.get("title"),
        "ruleGroup": rule.get("ruleGroup"),
        "folderUID": rule.get("folderUID"),
        "condition": rule.get("condition"),
        "for": parse_duration(rule.get("for")),
        "noDataState": rule.get("noDataState"),
        "execErrState": rule.get("execErrState"),
        "labels": rule.get("labels") or {},
        "annotations": rule.get("annotations") or {},
        "data": data,
    }


# --------------------------------------------------------------------------- #
# Summary reporting
# --------------------------------------------------------------------------- #


class Summary:
    def __init__(self):
        self.rows = []

    def add(self, resource, name, action, detail=""):
        self.rows.append((resource, name, action, detail))

    def render(self):
        headers = ("Resource", "Name", "Action", "Detail")
        table = [headers] + [tuple(str(c) for c in row) for row in self.rows]
        widths = [
            max(len(row[column]) for row in table) for column in range(len(headers))
        ]
        lines = []
        for position, row in enumerate(table):
            line = "  ".join(
                cell.ljust(widths[column]) for column, cell in enumerate(row)
            ).rstrip()
            lines.append(line)
            if position == 0:
                lines.append("  ".join("-" * width for width in widths))
        return "\n".join(lines)


# --------------------------------------------------------------------------- #
# Provisioning steps
# --------------------------------------------------------------------------- #


def ensure_folder(client, summary):
    existing = client.get_optional(f"/api/folders/{FOLDER_UID}")
    if existing is not None and existing.get("title") != FOLDER_TITLE:
        # Never repurpose a folder we did not create.
        raise ProvisionError(
            f"folder uid '{FOLDER_UID}' already exists with title "
            f"'{existing.get('title')}'; refusing to rename a foreign folder"
        )

    if existing is None:
        folders = client.get("/api/folders")[1] or []
        for folder in folders:
            if folder.get("title") == FOLDER_TITLE:
                existing = folder
                break

    if existing is None:
        status, body = client.call(
            "POST",
            "/api/folders",
            {"uid": FOLDER_UID, "title": FOLDER_TITLE},
        )
        uid = (body or {}).get("uid", FOLDER_UID)
        summary.add("folder", FOLDER_TITLE, "created", f"uid={uid} status={status}")
        return uid

    uid = existing.get("uid", FOLDER_UID)
    summary.add("folder", FOLDER_TITLE, "unchanged", f"uid={uid}")
    return uid


def provision_dashboards(client, folder_uid, summary):
    if not os.path.isdir(DASHBOARD_DIR):
        raise ProvisionError(f"dashboard directory not found: {DASHBOARD_DIR}")

    names = sorted(
        name for name in os.listdir(DASHBOARD_DIR) if name.endswith(".json")
    )
    if not names:
        raise ProvisionError(f"no dashboard JSON files in {DASHBOARD_DIR}")

    for name in names:
        path = os.path.join(DASHBOARD_DIR, name)
        with open(path, "r", encoding="utf-8") as handle:
            dashboard = json.load(handle)

        uid = dashboard.get("uid")
        if not uid:
            raise ProvisionError(f"{name}: dashboard has no uid")
        title = dashboard.get("title", uid)

        panels, targets = inject_datasource(dashboard, PROM_DATASOURCE)

        live = client.get_optional(f"/api/dashboards/uid/{uid}")
        if live is not None:
            live_folder = (live.get("meta") or {}).get("folderUid")
            if live_folder not in (None, "", folder_uid):
                # Never move a dashboard we did not create into our folder.
                raise ProvisionError(
                    f"dashboard uid '{uid}' already exists in folder "
                    f"'{live_folder}'; refusing to overwrite a foreign dashboard"
                )
            live_dashboard = (live.get("dashboard") or {})
            if strip_volatile(live_dashboard) == strip_volatile(dashboard):
                summary.add(
                    "dashboard",
                    title,
                    "unchanged",
                    f"uid={uid} panels+{panels} targets+{targets}",
                )
                continue

        status, _ = client.call(
            "POST",
            "/api/dashboards/db",
            {
                "dashboard": dashboard,
                "folderUid": folder_uid,
                "overwrite": True,
                "message": DASHBOARD_MESSAGE,
            },
        )
        action = "created" if live is None else "updated"
        summary.add(
            "dashboard",
            title,
            action,
            f"uid={uid} panels+{panels} targets+{targets} status={status}",
        )


def ensure_contact_point(client, summary):
    contact_points = client.get("/api/v1/provisioning/contact-points")[1] or []
    existing = None
    for point in contact_points:
        if point.get("name") == CONTACT_POINT_NAME:
            existing = point
            break

    body = {
        "name": CONTACT_POINT_NAME,
        "type": "email",
        "settings": {
            "addresses": CONTACT_POINT_EMAIL,
            "singleEmail": False,
        },
        "disableResolveMessage": False,
    }

    if existing is None:
        status, _ = client.call(
            "POST",
            "/api/v1/provisioning/contact-points",
            body,
            extra_headers=WRITE_HEADERS,
        )
        # Re-read to learn the uid Grafana assigned.
        refreshed = client.get("/api/v1/provisioning/contact-points")[1] or []
        uid = next(
            (p.get("uid") for p in refreshed if p.get("name") == CONTACT_POINT_NAME),
            "",
        )
        summary.add(
            "contact point",
            CONTACT_POINT_NAME,
            "created",
            f"uid={uid} type=email status={status}",
        )
        return uid

    uid = existing.get("uid", "")
    settings = existing.get("settings") or {}
    unchanged = (
        existing.get("type") == "email"
        and settings.get("addresses") == CONTACT_POINT_EMAIL
        and bool(settings.get("singleEmail", False)) is False
        and bool(existing.get("disableResolveMessage", False)) is False
    )
    if unchanged:
        summary.add("contact point", CONTACT_POINT_NAME, "unchanged", f"uid={uid}")
        return uid

    update = dict(body)
    update["uid"] = uid
    status, _ = client.call(
        "PUT",
        f"/api/v1/provisioning/contact-points/{uid}",
        update,
        extra_headers=WRITE_HEADERS,
    )
    summary.add(
        "contact point",
        CONTACT_POINT_NAME,
        "updated",
        f"uid={uid} type=email status={status}",
    )
    return uid


def provision_alert_rules(client, folder_uid, org_id, summary):
    with open(RULES_PATH, "r", encoding="utf-8") as handle:
        group_name, rules = parse_rules_yaml(handle.read())

    if group_name != RULE_GROUP:
        summary.add(
            "alert rules",
            group_name,
            "note",
            f"group name taken from rules.yaml (expected '{RULE_GROUP}')",
        )

    existing_rules = client.get("/api/v1/provisioning/alert-rules")[1] or []
    by_title = {}
    for rule in existing_rules:
        key = (rule.get("folderUID"), rule.get("ruleGroup"), rule.get("title"))
        by_title[key] = rule

    created = updated = unchanged = 0
    for rule in rules:
        body = build_alert_rule(rule, folder_uid, org_id, group_name)
        key = (folder_uid, group_name, body["title"])
        existing = by_title.get(key)

        if existing is None:
            status, _ = client.call(
                "POST",
                "/api/v1/provisioning/alert-rules",
                body,
                extra_headers=WRITE_HEADERS,
            )
            created += 1
            summary.add(
                "alert rule",
                body["title"],
                "created",
                f"severity={rule['severity']} for={rule['for']} status={status}",
            )
            continue

        existing_uid = existing.get("uid") or body["uid"]
        if rule_fingerprint(existing) == rule_fingerprint(body):
            unchanged += 1
            summary.add(
                "alert rule",
                body["title"],
                "unchanged",
                f"uid={existing_uid} severity={rule['severity']} for={rule['for']}",
            )
            continue

        update = dict(body)
        update["uid"] = existing_uid
        status, _ = client.call(
            "PUT",
            f"/api/v1/provisioning/alert-rules/{existing_uid}",
            update,
            extra_headers=WRITE_HEADERS,
        )
        updated += 1
        summary.add(
            "alert rule",
            body["title"],
            "updated",
            f"uid={existing_uid} severity={rule['severity']} for={rule['for']} status={status}",
        )

    return {
        "created": created,
        "updated": updated,
        "unchanged": unchanged,
        "total": len(rules),
    }


def ensure_notification_policy(client, summary):
    _, policy = client.get("/api/v1/provisioning/policies")
    if not isinstance(policy, dict):
        raise ProvisionError(
            f"unexpected notification policy payload: {policy!r}"
        )

    desired = json.loads(json.dumps(policy))  # deep copy; never mutate the original
    desired["receiver"] = CONTACT_POINT_NAME

    if policy.get("receiver") == CONTACT_POINT_NAME:
        summary.add(
            "notification policy",
            "root receiver",
            "unchanged",
            f"receiver={CONTACT_POINT_NAME} routes={len(policy.get('routes') or [])}",
        )
        return

    status, _ = client.call(
        "PUT",
        "/api/v1/provisioning/policies",
        desired,
        extra_headers=WRITE_HEADERS,
    )
    summary.add(
        "notification policy",
        "root receiver",
        "updated",
        f"receiver={CONTACT_POINT_NAME} routes={len(desired.get('routes') or [])} status={status}",
    )


# --------------------------------------------------------------------------- #
# Entry point
# --------------------------------------------------------------------------- #


def main():
    env = read_env(ENV_PATH)
    base_url = (env.get("GRAFANA_API_URL") or "").strip()
    token = (env.get("GRAFANA_API_TOKEN") or "").strip()

    if not base_url:
        raise ProvisionError("GRAFANA_API_URL is missing from .env")
    if not token:
        raise ProvisionError("GRAFANA_API_TOKEN is missing from .env")

    client = GrafanaClient(base_url, token)
    print(f"Grafana: {base_url}")
    print("Auth:    service-account bearer token loaded from .env (not printed)")
    print()

    # Fail fast and loudly on a bad credential before writing anything.
    client.get("/api/datasources")

    org = client.get("/api/org")[1] or {}
    org_id = org.get("id", 1)

    summary = Summary()
    folder_uid = ensure_folder(client, summary)
    provision_dashboards(client, folder_uid, summary)
    ensure_contact_point(client, summary)
    counts = provision_alert_rules(client, folder_uid, org_id, summary)
    ensure_notification_policy(client, summary)

    print(summary.render())
    print()
    print(
        "Alert rules: "
        f"{counts['created']} created, {counts['updated']} updated, "
        f"{counts['unchanged']} unchanged (of {counts['total']})."
    )
    print("Done. No delete request was issued.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except ProvisionError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("ERROR: interrupted", file=sys.stderr)
        sys.exit(130)
