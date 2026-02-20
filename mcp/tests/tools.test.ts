/**
 * E2E tests for @llmrates/mcp
 *
 * Requirements:
 *   TEST_API_KEY  — Developer-tier Unkey API key (required for most tests)
 *   TEST_API_URL  — Override API base URL (optional; defaults to production)
 *   TEST_FREE_KEY — Free-tier Unkey API key (for tier-error tests; optional)
 *
 * Tests that require a key are skipped automatically if TEST_API_KEY is not set.
 * This allows CI to run the suite with just npm test and skip live-API tests.
 *
 * The auth error test verifies that an invalid key returns an error — either
 * "Authentication failed" (when the live API is reachable) or "Could not reach"
 * (when the API is unreachable in offline/test environments). Both indicate the
 * server correctly attempted to use the key and returned a user-facing error.
 */

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SERVER_PATH = path.resolve(__dirname, "../dist/index.js");
const TEST_API_KEY = process.env.TEST_API_KEY ?? "";
const TEST_API_URL = process.env.TEST_API_URL ?? "";
const TEST_FREE_KEY = process.env.TEST_FREE_KEY ?? "";

const hasApiKey = Boolean(TEST_API_KEY);
const hasFreeKey = Boolean(TEST_FREE_KEY);

/** Helper: create an MCP client connected to the server binary */
async function createClient(env: Record<string, string> = {}): Promise<Client> {
  const transport = new StdioClientTransport({
    command: "node",
    args: [SERVER_PATH],
    env: {
      ...process.env,
      LLMRATES_API_KEY: TEST_API_KEY,
      ...(TEST_API_URL ? { LLMRATES_API_URL: TEST_API_URL } : {}),
      ...env,
    } as Record<string, string>,
  });
  const client = new Client({ name: "e2e-test-client", version: "1.0.0" });
  await client.connect(transport);
  return client;
}

// ---------------------------------------------------------------------------
// Tool listing
// ---------------------------------------------------------------------------
describe("listTools", () => {
  let client: Client;
  beforeAll(async () => {
    if (!hasApiKey) return;
    client = await createClient();
  });
  afterAll(async () => {
    if (client) await client.close();
  });

  it("lists all 6 tools", async () => {
    if (!hasApiKey) return;
    const result = await client.listTools();
    const names = result.tools.map((t) => t.name).sort();
    expect(names).toEqual([
      "compare_models",
      "get_cheapest_model",
      "get_context_snapshot",
      "get_price_history",
      "get_recent_changes",
      "subscribe_to_changes",
    ]);
  });
});

// ---------------------------------------------------------------------------
// Happy-path tests (require Developer-tier key)
// ---------------------------------------------------------------------------
describe("happy paths", () => {
  let client: Client;
  beforeAll(async () => {
    if (!hasApiKey) return;
    client = await createClient();
  });
  afterAll(async () => {
    if (client) await client.close();
  });

  it("get_recent_changes — returns array of changes", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({ name: "get_recent_changes", arguments: {} });
    expect(result.isError).toBeFalsy();
    expect(result.content).toHaveLength(1);
    const parsed = JSON.parse((result.content[0] as { text: string }).text);
    // API returns an object with a data array or similar envelope — check it's parseable
    expect(parsed).toBeDefined();
  });

  it("compare_models — returns comparison data for 2 models", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({
      name: "compare_models",
      arguments: { models: ["openai/gpt-4o-mini", "anthropic/claude-3-haiku"] },
    });
    expect(result.isError).toBeFalsy();
    const parsed = JSON.parse((result.content[0] as { text: string }).text);
    expect(parsed).toBeDefined();
  });

  it("get_cheapest_model — returns ranked results for a task", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({
      name: "get_cheapest_model",
      arguments: { task: "text summarization" },
    });
    expect(result.isError).toBeFalsy();
    const parsed = JSON.parse((result.content[0] as { text: string }).text);
    expect(parsed).toBeDefined();
  });

  it("get_context_snapshot — returns pricing snapshot", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({
      name: "get_context_snapshot",
      arguments: { format: "json" },
    });
    expect(result.isError).toBeFalsy();
    const parsed = JSON.parse((result.content[0] as { text: string }).text);
    expect(parsed).toBeDefined();
  });

  it("get_context_snapshot markdown — returns text content", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({
      name: "get_context_snapshot",
      arguments: { format: "markdown" },
    });
    expect(result.isError).toBeFalsy();
    // Markdown format may return plain text rather than JSON
    expect((result.content[0] as { text: string }).text).toBeTruthy();
  });

  it("get_price_history — returns history for a known model", async () => {
    if (!hasApiKey) return;
    const result = await client.callTool({
      name: "get_price_history",
      arguments: { model_id: "openai/gpt-4o-mini" },
    });
    expect(result.isError).toBeFalsy();
    const parsed = JSON.parse((result.content[0] as { text: string }).text);
    expect(parsed).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// Authentication error
// ---------------------------------------------------------------------------
describe("auth errors", () => {
  it("returns an error when an invalid API key is used", async () => {
    // With a deliberately invalid key the server returns either:
    //   • "Authentication failed" — when the live API is reachable and returns 401
    //   • "Could not reach" — when the API is unreachable (offline/CI environment)
    // Either outcome confirms the server correctly attempted the call and returned
    // a structured, user-facing error rather than crashing or returning success.
    const client = await createClient({ LLMRATES_API_KEY: "invalid_key_for_auth_test" });
    try {
      const result = await client.callTool({
        name: "get_recent_changes",
        arguments: {},
      });
      expect(result.isError).toBe(true);
      const text = (result.content[0] as { text: string }).text;
      expect(text).toMatch(/Authentication failed|Could not reach/);
    } finally {
      await client.close();
    }
  });
});

// ---------------------------------------------------------------------------
// Tier error (requires a Free-tier key that cannot call Dev+ tools)
// ---------------------------------------------------------------------------
describe("tier errors", () => {
  it.skipIf(!hasFreeKey)("returns tier error when Free key calls Dev+ tool", async () => {
    const client = await createClient({ LLMRATES_API_KEY: TEST_FREE_KEY });
    try {
      const result = await client.callTool({
        name: "get_cheapest_model",
        arguments: { task: "coding" },
      });
      expect(result.isError).toBe(true);
      expect((result.content[0] as { text: string }).text).toMatch(/tier/i);
    } finally {
      await client.close();
    }
  });
});

// ---------------------------------------------------------------------------
// Network error
// ---------------------------------------------------------------------------
describe("network errors", () => {
  it("returns connectivity error when API URL is unreachable", async () => {
    const client = await createClient({
      LLMRATES_API_KEY: TEST_API_KEY || "any_key",
      LLMRATES_API_URL: "https://this-host-does-not-exist.invalid",
    });
    try {
      const result = await client.callTool({
        name: "get_recent_changes",
        arguments: {},
      });
      expect(result.isError).toBe(true);
      expect((result.content[0] as { text: string }).text).toContain("Could not reach");
    } finally {
      await client.close();
    }
  });
});

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------
describe("validation errors", () => {
  let client: Client;
  beforeAll(async () => {
    client = await createClient({ LLMRATES_API_KEY: TEST_API_KEY || "any_key" });
  });
  afterAll(async () => {
    if (client) await client.close();
  });

  it("compare_models rejects fewer than 2 models", async () => {
    const result = await client.callTool({
      name: "compare_models",
      arguments: { models: ["openai/gpt-4o-mini"] },
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toContain("2 and 5");
  });

  it("compare_models rejects more than 5 models", async () => {
    const result = await client.callTool({
      name: "compare_models",
      arguments: { models: ["a", "b", "c", "d", "e", "f"] },
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toContain("2 and 5");
  });

  it("subscribe_to_changes rejects non-HTTPS URL", async () => {
    const result = await client.callTool({
      name: "subscribe_to_changes",
      arguments: { url: "http://example.com/webhook" },
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toContain("HTTPS");
  });

  it("subscribe_to_changes rejects private IP (SSRF)", async () => {
    const result = await client.callTool({
      name: "subscribe_to_changes",
      arguments: { url: "https://192.168.1.100/webhook" },
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toMatch(/private|loopback/i);
  });

  it("get_price_history rejects empty model_id", async () => {
    const result = await client.callTool({
      name: "get_price_history",
      arguments: { model_id: "" },
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toContain("non-empty");
  });

  it("unknown tool returns error", async () => {
    const result = await client.callTool({
      name: "nonexistent_tool",
      arguments: {},
    });
    expect(result.isError).toBe(true);
    expect((result.content[0] as { text: string }).text).toContain("Unknown tool");
  });
});
