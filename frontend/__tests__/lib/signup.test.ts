import { describe, it, expect, vi, beforeEach } from "vitest";
import { requestMagicLink, getIdentity, issueKey, regenerateKey } from "@/lib/signup";

// ── Mock fetch ────────────────────────────────────────────────────────────────

const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

beforeEach(() => mockFetch.mockReset());

// ── requestMagicLink ──────────────────────────────────────────────────────────

describe("requestMagicLink", () => {
  it("returns ok:true on 200", async () => {
    mockFetch.mockResolvedValueOnce({ ok: true });
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(true);
    expect(mockFetch).toHaveBeenCalledWith("/auth/signup/request-link", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ email: "test@example.com" }),
    }));
  });

  it("maps 429 with cooldown body to cooldown error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      json: async () => ({ error: "email cooldown active", retry_after_ms: 30_000 }),
    });
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("cooldown");
      expect(result.error.retryAfterMs).toBe(30_000);
    }
  });

  it("maps 429 without cooldown keyword to rate_limited error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      json: async () => ({ error: "too many requests" }),
    });
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("rate_limited");
    }
  });

  it("maps 422 with disposable keyword to disposable_domain error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 422,
      json: async () => ({ error: "disposable email domain not allowed" }),
    });
    const result = await requestMagicLink("test@mailinator.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("disposable_domain");
    }
  });

  it("maps 400 to invalid_email error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ error: "invalid email format" }),
    });
    const result = await requestMagicLink("notanemail");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("invalid_email");
    }
  });

  it("returns unknown error on network failure", async () => {
    mockFetch.mockRejectedValueOnce(new Error("network down"));
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("unknown");
    }
  });
});

// ── getIdentity ───────────────────────────────────────────────────────────────

describe("getIdentity", () => {
  it("returns identity on 200", async () => {
    const identity = {
      id: "id-1",
      email: "user@example.com",
      email_verified: true,
      has_active_key: false,
    };
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => identity });
    const result = await getIdentity();
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.identity.email).toBe("user@example.com");
    }
  });

  it("returns error on 401", async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 });
    const result = await getIdentity();
    expect(result.ok).toBe(false);
  });
});

// ── issueKey ──────────────────────────────────────────────────────────────────

describe("issueKey", () => {
  it("returns key plaintext on 200", async () => {
    const keyData = { plaintext: "sk-llmr-abc123", provider_key_id: "key_id_1" };
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => keyData });
    const result = await issueKey();
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.key.plaintext).toBe("sk-llmr-abc123");
    }
  });

  it("returns error on non-200", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: "active key already exists" }),
    });
    const result = await issueKey();
    expect(result.ok).toBe(false);
  });
});

// ── regenerateKey ─────────────────────────────────────────────────────────────

describe("regenerateKey", () => {
  it("returns new key plaintext on 200", async () => {
    const keyData = { plaintext: "sk-llmr-newkey456", provider_key_id: "key_id_2" };
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => keyData });
    const result = await regenerateKey();
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.key.plaintext).toBe("sk-llmr-newkey456");
    }
  });

  it("returns error when regenerate is rate-limited", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      json: async () => ({ error: "regenerate cooldown active", retry_after_ms: 60_000 }),
    });
    const result = await regenerateKey();
    expect(result.ok).toBe(false);
  });
});
