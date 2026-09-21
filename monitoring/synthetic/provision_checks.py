#!/usr/bin/env python3
"""Reconcile Grafana Cloud Synthetic Monitoring checks for the LLM Pricing API.

Public-probe HTTP checks only. The API has a public domain; the worker does not,
and is already covered by the `LLMScrapeTargetDown` alert from the internal
scrape, so probing it externally would expose `/health` for no gain.

This is a separate script (and a separate credential) from
`monitoring/provision/provision.py`: the Grafana instance API token is rejected
by the Synthetic Monitoring API with `403 invalid API token` — SM has its own
access token.

Usage
-----
    python3 monitoring/synthetic/provision_checks.py              # create/update
    python3 monitoring/synthetic/provision_checks.py --list       # show checks
    python3 monitoring/synthetic/provision_checks.py --delete JOB # remove one
    python3 monitoring/synthetic/provision_checks.py --live-fire  # temp failing check

`--live-fire` creates a check pointed at a path that 404s, so it fails on
purpose. Its only purpose is to prove that a failing probe produces a delivered
notification; delete it again with
`--delete llm-pricing-livefire`.

Required environment (repository-root `.env`, never the shell):

| Variable | Purpose |
|---|---|
| `GRAFANA_SM_API_URL` | Region-specific SM API base, e.g. `https://synthetic-monitoring-api-eu-west-2.grafana.net` |
| `GRAFANA_SM_ACCESS_TOKEN` | SM access token (Config -> Access tokens) |
| `GRAFANA_SM_STACK_ID` | Hosted-graphs stack id |

Cost: one HTTP check at a 5-minute interval from one probe is
`1 x 1 x 1 x (43,200 / 5)` = 8,640 test executions per month, i.e. 0.86 of the
per-10,000 billing unit. Each execution also produces a handful of active
series, billed at standard metrics rates. Run the SM UI's check calculator to
see the exact numbers for your plan before adding more checks.
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
ENV_PATH = os.path.join(REPO_ROOT, ".env")

# --------------------------------------------------------------------------- #
# Check definitions
# --------------------------------------------------------------------------- #

# Probe selection: keep it close to the service so the check is an uptime
# signal, not an over-the-internet latency measurement (see the Grafana cost
# guidance). Override with --probe.
PREFERRED_PROBES = ("London", "Frankfurt", "Amsterdam", "Paris")

# /health returns 200 only when both Postgres and Redis answer within the
# checker's deadline, and 503 otherwise — so the status code alone is the
# assertion. No body regex: it would be brittle for no extra signal.
HEALTH_CHECK = {
    "job": "llm-pricing-api-health",
    "target": "https://api.llmrates.live/health",
    "frequency": 300000,  # ms -> 5 minutes
    "timeout": 10000,  # ms
    "enabled": True,
    "alertSensitivity": "none",  # alerting lives in monitoring/alerts/rules.yaml
    "labels": [{"name": "service", "value": "llm-pricing-api"}],
    "settings": {
        "http": {
            "method": "GET",
            "ipVersion": "V4",
            "validStatusCodes": [200],
            "noFollowRedirects": False,
            "cacheBusting": False,
            "headers": [],
            "body": "",
        }
    },
}

# Deliberately failing check for the live-fire notification proof. A 404 path
# is used rather than a bogus host so the failure is a fast assertion failure,
# not a DNS/TLS error.
LIVE_FIRE_CHECK = {
    "job": "llm-pricing-livefire",
    "target": "https://api.llmrates.live/__livefire-should-404",
    "frequency": 300000,
    "timeout": 10000,
    "enabled": True,
    "alertSensitivity": "none",
    "labels": [{"name": "service", "value": "livefire"}],
    "settings": {
        "http": {
            "method": "GET",
            "ipVersion": "V4",
            "validStatusCodes": [200],
            "noFollowRedirects": False,
            "cacheBusting": False,
            "headers": [],
            "body": "",
        }
    },
}


class ProvisionError(RuntimeError):
    """Raised for a configuration or API error worth stopping for."""


# --------------------------------------------------------------------------- #
# .env + HTTP plumbing
# --------------------------------------------------------------------------- #


def read_env(path=ENV_PATH):
    """Parse the repository .env, splitting each line on the first '='.

    Not shell-sourced, and no value is ever printed — the token must not reach
    logs or a traceback.
    """
    env = {}
    try:
        with open(path, encoding="utf-8") as handle:
            for line in handle:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                env[key.strip()] = value.strip().strip("'\"")
    except FileNotFoundError:
        raise ProvisionError(f"{path} not found")
    return env


class SMClient:
    """Minimal Synthetic Monitoring API client (standard library only)."""

    def __init__(self, base_url, token):
        # The Grafana Cloud UI shows this host without a scheme; accept either.
        base_url = base_url.strip().rstrip("/")
        if not base_url.startswith(("http://", "https://")):
            base_url = f"https://{base_url}"
        self.base_url = base_url
        self.token = token

    def request(self, method, path, body=None):
        url = f"{self.base_url}{path}"
        data = json.dumps(body).encode("utf-8") if body is not None else None
        request = urllib.request.Request(url, data=data, method=method)
        request.add_header("Authorization", f"Bearer {self.token}")
        if data is not None:
            request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                raw = response.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            detail = error.read().decode("utf-8", "replace")[:400]
            raise ProvisionError(f"{method} {path} -> HTTP {error.code}: {detail}")
        except urllib.error.URLError as error:
            raise ProvisionError(f"{method} {path} -> {error.reason}")

    def list_checks(self):
        return self.request("GET", "/api/v1/check") or []

    def list_probes(self):
        # Singular: the collection route is /api/v1/probe, not /probes.
        return self.request("GET", "/api/v1/probe") or []

    def create_check(self, check):
        return self.request("POST", "/api/v1/check", check)

    def update_check(self, check_id, check):
        return self.request("PUT", f"/api/v1/check/{check_id}", check)

    def delete_check(self, check_id):
        return self.request("DELETE", f"/api/v1/check/{check_id}")


# --------------------------------------------------------------------------- #
# Reconciliation
# --------------------------------------------------------------------------- #


def select_probe(probes, wanted=None):
    """Return the probe ids to run a check from."""
    if not probes:
        raise ProvisionError("no probes available from /api/v1/probes")
    if wanted:
        for probe in probes:
            if probe.get("name") == wanted:
                return [probe["id"]]
        names = ", ".join(sorted(p.get("name", "?") for p in probes))
        raise ProvisionError(f"no probe named {wanted!r}; available: {names}")
    for preferred in PREFERRED_PROBES:
        for probe in probes:
            if preferred.lower() in (probe.get("name") or "").lower():
                return [probe["id"]]
    return [probes[0]["id"]]


def desired_checks(live_fire=False):
    return [LIVE_FIRE_CHECK] if live_fire else [HEALTH_CHECK]


def reconcile(client, probe_ids, live_fire=False):
    """Create or update each desired check, matched by job name."""
    existing = {check.get("job"): check for check in client.list_checks()}
    results = []

    for spec in desired_checks(live_fire=live_fire):
        body = dict(spec, probes=probe_ids)
        current = existing.get(spec["job"])
        if current is None:
            client.create_check(body)
            results.append(("created", spec["job"], spec["target"]))
            continue

        if checks_equal(current, body):
            results.append(("unchanged", spec["job"], spec["target"]))
            continue

        client.update_check(current["id"], body)
        results.append(("updated", spec["job"], spec["target"]))

    return results


def checks_equal(current, desired):
    """Compare only the fields this script owns.

    The API returns server-side defaults and computed fields, so a naive
    whole-object comparison would report a change on every run.
    """
    keys = ("job", "target", "frequency", "timeout", "enabled", "probes", "labels")
    for key in keys:
        if normalise(current.get(key)) != normalise(desired.get(key)):
            return False
    current_http = ((current.get("settings") or {}).get("http")) or {}
    desired_http = ((desired.get("settings") or {}).get("http")) or {}
    for key in ("method", "ipVersion", "validStatusCodes", "noFollowRedirects"):
        if normalise(current_http.get(key)) != normalise(desired_http.get(key)):
            return False
    return True


def normalise(value):
    if isinstance(value, list) and all(isinstance(item, dict) for item in value):
        return sorted(json.dumps(item, sort_keys=True) for item in value)
    return value


def delete_check(client, job):
    for check in client.list_checks():
        if check.get("job") == job:
            client.delete_check(check["id"])
            return check.get("id")
    return None


def print_summary(results):
    if not results:
        print("No checks reconciled.")
        return
    print(f"{'Action':<10} {'Job':<26} Target")
    print(f"{'-' * 10} {'-' * 26} {'-' * 40}")
    for action, job, target in results:
        print(f"{action:<10} {job:<26} {target}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--list", action="store_true", help="list existing checks and exit")
    parser.add_argument("--delete", metavar="JOB", help="delete the check with this job name")
    parser.add_argument("--probe", metavar="NAME", help="probe location name to run from")
    parser.add_argument(
        "--live-fire",
        action="store_true",
        help="create the deliberately failing live-fire check instead",
    )
    args = parser.parse_args()

    try:
        env = read_env()
        base_url = (env.get("GRAFANA_SM_API_URL") or "").strip()
        token = (env.get("GRAFANA_SM_ACCESS_TOKEN") or "").strip()
        if not base_url or not token:
            raise ProvisionError(
                "GRAFANA_SM_API_URL and GRAFANA_SM_ACCESS_TOKEN must be set in .env "
                "(see .env.example; the SM token is separate from GRAFANA_API_TOKEN)"
            )
        client = SMClient(base_url, token)

        if args.list:
            for check in client.list_checks():
                print(
                    f"{check.get('job'):<26} {check.get('target'):<48} "
                    f"{check.get('frequency')}ms enabled={check.get('enabled')}"
                )
            return 0

        if args.delete:
            deleted = delete_check(client, args.delete)
            if deleted is None:
                print(f"No check named {args.delete!r}.")
                return 0
            print(f"Deleted {args.delete} (id {deleted}).")
            return 0

        probe_ids = select_probe(client.list_probes(), args.probe)
        print(f"SM API: {base_url}")
        print(f"Probe:  {probe_ids}")
        print()
        print_summary(reconcile(client, probe_ids, live_fire=args.live_fire))
        if args.live_fire:
            print(
                "\nLive-fire check created. It fails by design; confirm a\n"
                "notification arrives, then remove it with:\n"
                "  python3 monitoring/synthetic/provision_checks.py --delete llm-pricing-livefire"
            )
        return 0
    except ProvisionError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
