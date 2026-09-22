/**
 * Client for the agent device-grant flow.
 *
 * The flow exists so the credential lands in the agent, not in the user's
 * clipboard. This module performs the two HTTP calls:
 *
 *   1. startGrant  → POST /auth/agent/device   (device_code + user_code)
 *   2. pollGrant   → POST /auth/agent/token    (pending / issued / denied / …)
 *
 * The API answers every expected poll outcome with HTTP 200 and a `status`
 * field, so a poll loop does not manufacture a pile of 4xx responses. Only a
 * malformed request or a genuinely refused one comes back as an error status,
 * and those are mapped to explicit outcomes below rather than thrown, so the
 * caller can tell "keep waiting" apart from "give up and tell the user why".
 */

export interface StartedGrant {
  deviceCode: string;
  userCode: string;
  verificationUri: string;
  verificationUriComplete: string;
  /** ISO 8601. */
  expiresAt: string;
  intervalSeconds: number;
}

export type PollOutcome =
  | { status: "issued"; apiKey: string; identityId?: string; email?: string; label?: string }
  | { status: "pending" }
  | { status: "denied" }
  | { status: "expired" }
  | { status: "redeemed" }
  | { status: "slow_down"; intervalSeconds?: number }
  /** The API does not recognise our stored device code; local state is stale. */
  | { status: "unknown" }
  | { status: "key_limit"; message: string }
  | { status: "error"; message: string; retryable: boolean };

/** Raised when a grant cannot even be started. The message is user-facing. */
export class DeviceFlowError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DeviceFlowError";
  }
}

export interface StartGrantOptions {
  baseUrl: string;
  clientName: string;
  platform?: string;
  fetchImpl?: typeof fetch;
}

/** Start a device grant. Returns the codes the user and the client each handle. */
export async function startGrant(options: StartGrantOptions): Promise<StartedGrant> {
  const { baseUrl, clientName, platform, fetchImpl = fetch } = options;
  const url = new URL("/auth/agent/device", ensureTrailingSlash(baseUrl)).toString();

  let response: Response;
  try {
    response = await fetchImpl(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify(platform ? { client_name: clientName, platform } : { client_name: clientName }),
    });
  } catch (err) {
    throw new DeviceFlowError(
      `Could not reach the LLM Rates API at ${baseUrl} to start authorization. ` +
        `Check your network connection. (${errorMessage(err)})`
    );
  }

  const body = await readBody(response);

  if (response.status === 503) {
    throw new DeviceFlowError(
      "The LLM Rates API has signup disabled, so it cannot issue a new key right now."
    );
  }
  if (response.status === 429) {
    throw new DeviceFlowError(
      "Too many authorization requests from this network. Wait a few minutes and try again."
    );
  }
  if (!response.ok) {
    throw new DeviceFlowError(
      `Could not start authorization (HTTP ${response.status})${detailSuffix(body)}`
    );
  }

  const deviceCode = stringField(body, "device_code");
  const userCode = stringField(body, "user_code");
  if (!deviceCode || !userCode) {
    throw new DeviceFlowError("The API returned an authorization response without codes.");
  }

  const expiresInSeconds = numberField(body, "expires_in") ?? 600;
  const intervalSeconds = numberField(body, "interval") ?? 5;

  return {
    deviceCode,
    userCode,
    verificationUri: stringField(body, "verification_uri") ?? "",
    verificationUriComplete: stringField(body, "verification_uri_complete") ?? "",
    expiresAt: new Date(Date.now() + expiresInSeconds * 1000).toISOString(),
    intervalSeconds,
  };
}

export interface PollGrantOptions {
  baseUrl: string;
  deviceCode: string;
  fetchImpl?: typeof fetch;
}

/**
 * Perform a single poll.
 *
 * Deliberately one poll per call: the caller owns the retry cadence and the
 * overall deadline, which keeps the waiting policy next to the thing that has to
 * explain it to the user.
 */
export async function pollGrant(options: PollGrantOptions): Promise<PollOutcome> {
  const { baseUrl, deviceCode, fetchImpl = fetch } = options;
  const url = new URL("/auth/agent/token", ensureTrailingSlash(baseUrl)).toString();

  let response: Response;
  try {
    response = await fetchImpl(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ device_code: deviceCode }),
    });
  } catch (err) {
    // Transport failures are transient by nature; let the caller retry until
    // the grant's own deadline passes.
    return { status: "error", message: `Could not reach the API: ${errorMessage(err)}`, retryable: true };
  }

  const body = await readBody(response);

  if (response.ok) {
    switch (stringField(body, "status")) {
      case "issued": {
        const apiKey = stringField(body, "api_key");
        if (!apiKey) {
          return { status: "error", message: "The API reported success without returning a key.", retryable: false };
        }
        return {
          status: "issued",
          apiKey,
          identityId: stringField(body, "identity_id"),
          email: stringField(body, "email"),
          label: stringField(body, "label"),
        };
      }
      case "pending":
        return { status: "pending" };
      case "denied":
        return { status: "denied" };
      case "expired":
        return { status: "expired" };
      case "redeemed":
        return { status: "redeemed" };
      case "slow_down":
        return { status: "slow_down", intervalSeconds: numberField(body, "interval") };
      default:
        return { status: "error", message: "The API returned an unrecognised status.", retryable: false };
    }
  }

  switch (response.status) {
    case 400:
      // The API does not know this device code, so whatever we stored is stale.
      return { status: "unknown" };
    case 409:
      return {
        status: "key_limit",
        message:
          stringField(body, "detail") ??
          "This account has reached its active key limit. Revoke a key and try again.",
      };
    case 429:
      // Rate limited: back off rather than surfacing an error the user cannot act on.
      return { status: "slow_down", intervalSeconds: 10 };
    case 503:
      return { status: "error", message: "The API has signup disabled right now.", retryable: false };
    default:
      return {
        status: "error",
        message: `Authorization poll failed (HTTP ${response.status})${detailSuffix(body)}`,
        retryable: response.status >= 500,
      };
  }
}

/** Resolves after ms. Kept here so callers do not each hand-roll a timer. */
export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ── Internals ─────────────────────────────────────────────────────────────────

function ensureTrailingSlash(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
}

async function readBody(response: Response): Promise<Record<string, unknown>> {
  try {
    const text = await response.text();
    if (!text) return {};
    const parsed = JSON.parse(text) as unknown;
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function stringField(body: Record<string, unknown>, key: string): string | undefined {
  const value = body[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function numberField(body: Record<string, unknown>, key: string): number | undefined {
  const value = body[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function detailSuffix(body: Record<string, unknown>): string {
  const detail = stringField(body, "detail");
  return detail ? `: ${detail}` : ".";
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
