/**
 * Unit tests for the agent authentication flow: local credential storage, the
 * device-grant HTTP client, and the `authenticate` tool's two phases.
 *
 * These run entirely offline against a stubbed fetch, so they need no API key
 * and no network. The security-relevant assertions are the file modes (0600 for
 * the secret, 0700 for its directory) and the fact that the raw device code is
 * never returned in a tool result.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { promises as fs } from "fs";
import os from "os";
import path from "path";

import {
  clearPendingGrant,
  configDir,
  credentialsPath,
  pendingGrantPath,
  readPendingGrant,
  resolveApiKey,
  writeApiKey,
  writePendingGrant,
} from "../src/config-store.js";
import { pollGrant, startGrant, DeviceFlowError } from "../src/device-flow.js";
import * as authenticate from "../src/tools/authenticate.js";
import { ApiClient } from "../src/api-client.js";

const BASE = "https://api.example.test";

let tmpDir: string;

beforeEach(async () => {
  tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "llmrates-test-"));
  vi.stubEnv("LLMRATES_CONFIG_DIR", tmpDir);
  vi.stubEnv("LLMRATES_API_KEY", "");
});

afterEach(async () => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  await fs.rm(tmpDir, { recursive: true, force: true });
});

/** Builds a fetch stub that dispatches on pathname and records the bodies. */
function stubFetch(
  routes: Record<string, (body: Record<string, unknown>, call: number) => { status: number; body: unknown }>
) {
  const calls: Record<string, number> = {};
  const seen: Array<{ path: string; body: Record<string, unknown> }> = [];

  const impl = vi.fn(async (input: unknown, init?: RequestInit) => {
    const url = new URL(String(input));
    const handler = routes[url.pathname];
    if (!handler) throw new Error(`unexpected request to ${url.pathname}`);
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
    calls[url.pathname] = (calls[url.pathname] ?? 0) + 1;
    seen.push({ path: url.pathname, body });
    const result = handler(body, calls[url.pathname]);
    return new Response(JSON.stringify(result.body), {
      status: result.status,
      headers: { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", impl);
  return { impl, seen };
}

// ── Config store ──────────────────────────────────────────────────────────────

describe("config-store", () => {
  it("resolves the directory from LLMRATES_CONFIG_DIR", () => {
    expect(configDir()).toBe(tmpDir);
    expect(credentialsPath()).toBe(path.join(tmpDir, "credentials.json"));
  });

  it("writes the key at 0600 in a 0700 directory", async () => {
    const target = await writeApiKey("llmr_secret_key");

    const fileMode = (await fs.stat(target)).mode & 0o777;
    // The secret must not be readable by group or other, ever.
    expect(fileMode).toBe(0o600);

    const dirMode = (await fs.stat(path.dirname(target))).mode & 0o777;
    expect(dirMode).toBe(0o700);
  });

  it("round-trips the stored key", async () => {
    await writeApiKey("llmr_round_trip");
    const resolved = await resolveApiKey();
    expect(resolved.source).toBe("file");
    expect(resolved.apiKey).toBe("llmr_round_trip");
  });

  it("prefers the environment over the stored file", async () => {
    await writeApiKey("llmr_from_file");
    vi.stubEnv("LLMRATES_API_KEY", "llmr_from_env");

    const resolved = await resolveApiKey();
    expect(resolved.source).toBe("env");
    expect(resolved.apiKey).toBe("llmr_from_env");
  });

  it("reports no key when neither source is present", async () => {
    const resolved = await resolveApiKey();
    expect(resolved.source).toBe("none");
    expect(resolved.apiKey).toBeNull();
  });

  it("treats a malformed credentials file as absent rather than throwing", async () => {
    await fs.writeFile(credentialsPath(), "{ this is not json", "utf8");
    const resolved = await resolveApiKey();
    expect(resolved.source).toBe("none");
  });

  it("round-trips and clears a pending grant", async () => {
    const grant = {
      deviceCode: "device-abc",
      userCode: "ACDF0001",
      verificationUri: `${BASE}/activate`,
      verificationUriComplete: `${BASE}/activate?code=ACDF-0001`,
      expiresAt: new Date(Date.now() + 60_000).toISOString(),
      baseUrl: BASE,
    };
    await writePendingGrant(grant);
    expect(await readPendingGrant()).toEqual(grant);

    const mode = (await fs.stat(pendingGrantPath())).mode & 0o777;
    // The device code is a credential, so it gets the same mode as the key.
    expect(mode).toBe(0o600);

    await clearPendingGrant();
    expect(await readPendingGrant()).toBeNull();
    // Clearing twice is not an error.
    await expect(clearPendingGrant()).resolves.toBeUndefined();
  });
});

// ── Device flow HTTP client ───────────────────────────────────────────────────

describe("startGrant", () => {
  it("returns the codes and derives an expiry", async () => {
    stubFetch({
      "/auth/agent/device": () => ({
        status: 200,
        body: {
          device_code: "raw-device",
          user_code: "ACDF-0001",
          verification_uri: `${BASE}/activate`,
          verification_uri_complete: `${BASE}/activate?code=ACDF-0001`,
          expires_in: 600,
          interval: 5,
        },
      }),
    });

    const grant = await startGrant({ baseUrl: BASE, clientName: "Claude Code" });
    expect(grant.deviceCode).toBe("raw-device");
    expect(grant.userCode).toBe("ACDF-0001");
    expect(grant.intervalSeconds).toBe(5);
    expect(Date.parse(grant.expiresAt)).toBeGreaterThan(Date.now());
  });

  it("surfaces signup-disabled and rate-limited responses as readable errors", async () => {
    stubFetch({ "/auth/agent/device": () => ({ status: 503, body: {} }) });
    await expect(startGrant({ baseUrl: BASE, clientName: "x" })).rejects.toThrow(DeviceFlowError);

    stubFetch({ "/auth/agent/device": () => ({ status: 429, body: {} }) });
    await expect(startGrant({ baseUrl: BASE, clientName: "x" })).rejects.toThrow(/too many/i);
  });

  it("rejects a response with no codes", async () => {
    stubFetch({ "/auth/agent/device": () => ({ status: 200, body: { expires_in: 600 } }) });
    await expect(startGrant({ baseUrl: BASE, clientName: "x" })).rejects.toThrow(/without codes/i);
  });
});

describe("pollGrant", () => {
  const poll = (status: number, body: unknown) => {
    stubFetch({ "/auth/agent/token": () => ({ status, body }) });
    return pollGrant({ baseUrl: BASE, deviceCode: "raw-device" });
  };

  it("maps a successful issuance", async () => {
    const outcome = await poll(200, {
      status: "issued",
      api_key: "llmr_new",
      identity_id: "id-1",
      email: "a@example.com",
      label: "Claude Code",
    });
    expect(outcome).toEqual({
      status: "issued",
      apiKey: "llmr_new",
      identityId: "id-1",
      email: "a@example.com",
      label: "Claude Code",
    });
  });

  it("maps the non-terminal and terminal statuses", async () => {
    await expect(poll(200, { status: "pending" })).resolves.toEqual({ status: "pending" });
    await expect(poll(200, { status: "denied" })).resolves.toEqual({ status: "denied" });
    await expect(poll(200, { status: "expired" })).resolves.toEqual({ status: "expired" });
    await expect(poll(200, { status: "redeemed" })).resolves.toEqual({ status: "redeemed" });
    await expect(poll(200, { status: "slow_down", interval: 10 })).resolves.toEqual({
      status: "slow_down",
      intervalSeconds: 10,
    });
  });

  it("treats a 400 as stale local state", async () => {
    await expect(poll(400, { detail: "unknown device_code" })).resolves.toEqual({ status: "unknown" });
  });

  it("treats a 409 as the account key limit", async () => {
    const outcome = await poll(409, { detail: "this account has reached its active key limit" });
    expect(outcome.status).toBe("key_limit");
  });

  it("treats a 429 as back-off rather than an error", async () => {
    const outcome = await poll(429, {});
    expect(outcome.status).toBe("slow_down");
  });

  it("marks a 5xx as retryable but a broken success payload as not", async () => {
    const server = await poll(500, {});
    expect(server).toMatchObject({ status: "error", retryable: true });

    const malformed = await poll(200, { status: "issued" });
    expect(malformed).toMatchObject({ status: "error", retryable: false });
  });
});

// ── authenticate tool ─────────────────────────────────────────────────────────

describe("authenticate tool", () => {
  function client() {
    return new ApiClient(BASE, "");
  }

  it("phase 1 returns the prompt and persists a pending grant", async () => {
    stubFetch({
      "/auth/agent/device": () => ({
        status: 200,
        body: {
          device_code: "raw-device",
          user_code: "ACDF-0001",
          verification_uri: `${BASE}/activate`,
          verification_uri_complete: `${BASE}/activate?code=ACDF-0001`,
          expires_in: 600,
          interval: 5,
        },
      }),
    });

    const result = await authenticate.handler({ client_name: "Claude Code" }, client());
    const text = (result.content[0] as { text: string }).text;

    expect(result.isError).toBeFalsy();
    // The user needs both the clickable URL and the code to cross-check.
    expect(text).toContain(`${BASE}/activate?code=ACDF-0001`);
    expect(text).toContain("ACDF-0001");

    const pending = await readPendingGrant();
    expect(pending?.deviceCode).toBe("raw-device");

    // The raw device code is a credential and must not leak into tool output.
    expect(text).not.toContain("raw-device");
  });

  it("phase 2 stores the key and installs it on the client", async () => {
    await writePendingGrant({
      deviceCode: "raw-device",
      userCode: "ACDF0001",
      verificationUri: `${BASE}/activate`,
      verificationUriComplete: `${BASE}/activate?code=ACDF-0001`,
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      baseUrl: BASE,
    });
    stubFetch({
      "/auth/agent/token": () => ({
        status: 200,
        body: { status: "issued", api_key: "llmr_collected", email: "a@example.com" },
      }),
    });

    const c = client();
    const result = await authenticate.handler({}, c);
    const text = (result.content[0] as { text: string }).text;

    expect(result.isError).toBeFalsy();
    expect(text).toContain(credentialsPath());

    const resolved = await resolveApiKey();
    expect(resolved.apiKey).toBe("llmr_collected");
    // Tools called later in this session must work without a restart.
    expect(c.hasApiKey()).toBe(true);
    // The pending grant is consumed.
    expect(await readPendingGrant()).toBeNull();
  });

  it("reports a denial without storing anything", async () => {
    await writePendingGrant({
      deviceCode: "raw-device",
      userCode: "ACDF0001",
      verificationUri: `${BASE}/activate`,
      verificationUriComplete: `${BASE}/activate?code=ACDF-0001`,
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      baseUrl: BASE,
    });
    stubFetch({ "/auth/agent/token": () => ({ status: 200, body: { status: "denied" } }) });

    const result = await authenticate.handler({}, client());
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toMatch(/denied/i);

    const resolved = await resolveApiKey();
    expect(resolved.apiKey).toBeNull();
    expect(await readPendingGrant()).toBeNull();
  });

  it("discards a pending grant that belongs to a different API base URL", async () => {
    await writePendingGrant({
      deviceCode: "raw-device",
      userCode: "ACDF0001",
      verificationUri: "https://other.example/activate",
      verificationUriComplete: "https://other.example/activate?code=ACDF-0001",
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      baseUrl: "https://other.example",
    });
    const { seen } = stubFetch({
      "/auth/agent/device": () => ({
        status: 200,
        body: {
          device_code: "fresh-device",
          user_code: "ACDF0002",
          verification_uri: `${BASE}/activate`,
          verification_uri_complete: `${BASE}/activate?code=ACDF-0002`,
          expires_in: 600,
          interval: 5,
        },
      }),
      "/auth/agent/token": () => ({ status: 200, body: { status: "pending" } }),
    });

    await authenticate.handler({}, client());

    // It must start a fresh grant rather than polling the other environment's.
    expect(seen.map((s) => s.path)).toEqual(["/auth/agent/device"]);
    expect((await readPendingGrant())?.deviceCode).toBe("fresh-device");
  });

  it("returns the prompt again when the request is still pending", async () => {
    await writePendingGrant({
      deviceCode: "raw-device",
      userCode: "ACDF0001",
      verificationUri: `${BASE}/activate`,
      verificationUriComplete: `${BASE}/activate?code=ACDF-0001`,
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      baseUrl: BASE,
    });
    stubFetch({ "/auth/agent/token": () => ({ status: 200, body: { status: "pending" } }) });

    const result = await authenticate.handler({ wait_seconds: 1 }, client());
    const text = (result.content[0] as { text: string }).text;

    expect(result.isError).toBeFalsy();
    expect(text).toContain("still waiting for approval");
    // State survives so the user can approve and the agent can retry.
    expect(await readPendingGrant()).not.toBeNull();
  });
});

// ── Storage location and permissions (issue #191 acceptance criteria) ────────

describe("credential storage location and permissions", () => {
  it("defaults to ~/.llmrates/credentials.json", () => {
    vi.stubEnv("LLMRATES_CONFIG_DIR", "");
    // The decided location on issue #191: predictable, and not dependent on
    // ambient XDG settings.
    expect(configDir()).toBe(path.join(os.homedir(), ".llmrates"));
    expect(credentialsPath()).toBe(path.join(os.homedir(), ".llmrates", "credentials.json"));
  });

  it("refuses to write into a directory other users can reach", async () => {
    const openDir = await fs.mkdtemp(path.join(os.tmpdir(), "llmrates-open-"));
    await fs.chmod(openDir, 0o755);
    vi.stubEnv("LLMRATES_CONFIG_DIR", openDir);

    await expect(writeApiKey("llmr_must_not_be_written")).rejects.toThrow(/chmod 700/);
    // Refusing means refusing: nothing may be left behind.
    await expect(fs.readFile(path.join(openDir, "credentials.json"), "utf8")).rejects.toThrow();
  });
});

describe("authenticate with an unwritable config directory", () => {
  it("reports the failure actionably and never puts the key in tool output", async () => {
    const dir = await fs.mkdtemp(path.join(os.tmpdir(), "llmrates-store-"));
    vi.stubEnv("LLMRATES_CONFIG_DIR", dir);

    await writePendingGrant({
      deviceCode: "raw-device",
      userCode: "ACDF0001",
      verificationUri: `${BASE}/activate`,
      verificationUriComplete: `${BASE}/activate?code=ACDF-0001`,
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      baseUrl: BASE,
    });
    // Reachable by other users, so persisting must be refused.
    await fs.chmod(dir, 0o755);

    stubFetch({
      "/auth/agent/token": () => ({
        status: 200,
        body: { status: "issued", api_key: "llmr_secret_value", email: "a@example.com" },
      }),
    });

    const c = new ApiClient(BASE, "");
    const result = await authenticate.handler({}, c);
    const text = (result.content[0] as { text: string }).text;

    expect(result.isError).toBe(true);
    expect(text).toMatch(/could not be saved/);
    // Actionable: names the fix.
    expect(text).toMatch(/chmod 700/);
    // The secret must never appear in tool output, even on a failure path.
    expect(text).not.toContain("llmr_secret_value");
    // The key was still installed for this session, so the user is not left
    // with nothing after approving.
    expect(c.hasApiKey()).toBe(true);
    // The consumed grant must not linger.
    expect(await readPendingGrant()).toBeNull();

    await fs.rm(dir, { recursive: true, force: true });
  });
});
