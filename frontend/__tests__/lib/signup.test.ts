import { describe, it, expect, vi, beforeEach } from "vitest";
import { requestMagicLink, getIdentity, issueKey, regenerateKey } from "@/lib/signup";

// ── Mock fetch ────────────────────────────────────────────────────────────────

const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

beforeEach(() => mockFetch.mockReset());

/** Helper to build a mock Response with headers support. */
function mockResponse(
  opts: { ok: boolean; status?: number; body?: Record<string, unknown> }
) {
  const hdrs = new Map<string, string>();
  return {
    ok: opts.ok,
    status: opts.status ?? (opts.ok ? 200 : 500),
    headers: { get: (name: string) => hdrs.get(name.toLowerCase()) ?? null, _map: hdrs },
    json: async () => opts.body ?? {},
  };
}

/** Same as mockResponse but also sets the Retry-After header. */
function mockResponseWithRetryAfter(
  opts: { ok: boolean; status: number; body?: Record<string, unknown>; retryAfterSec?: number }
) {
  const resp = mockResponse(opts);
  if (opts.retryAfterSec !== undefined) {
    (resp.headers._map as Map<string, string>).set("retry-after", String(opts.retryAfterSec));
  }
  return resp;
}

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

  it("maps 429 with cooldown body (RFC7807 detail) to cooldown error", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponse({
        ok: false,
        status: 429,
        body: { detail: "email cooldown active", extensions: { retryAfterMs: 30_000 } },
      })
    );
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("cooldown");
      expect(result.error.retryAfterMs).toBe(30_000);
    }
  });

  it("maps 429 with Retry-After header to rate_limited error", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponseWithRetryAfter({
        ok: false,
        status: 429,
        body: { detail: "too many requests" },
        retryAfterSec: 60,
      })
    );
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("rate_limited");
      expect(result.error.retryAfterMs).toBe(60_000);
    }
  });

  it("maps 429 without cooldown keyword to rate_limited error", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponse({ ok: false, status: 429, body: { detail: "too many requests" } })
    );
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("rate_limited");
    }
  });

  it("falls back to legacy body.error when detail is absent", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponse({ ok: false, status: 429, body: { error: "cooldown active", retry_after_ms: 5_000 } })
    );
    const result = await requestMagicLink("test@example.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("cooldown");
      expect(result.error.message).toBe("cooldown active");
      expect(result.error.retryAfterMs).toBe(5_000);
    }
  });

  it("maps 422 with disposable keyword to disposable_domain error", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponse({ ok: false, status: 422, body: { detail: "disposable email domain not allowed" } })
    );
    const result = await requestMagicLink("test@mailinator.com");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("disposable_domain");
    }
  });

  it("maps 400 to invalid_email error", async () => {
    mockFetch.mockResolvedValueOnce(
      mockResponse({ ok: false, status: 400, body: { detail: "invalid email format" } })
    );
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
    mockFetch.mockResolvedValueOnce(
      mockResponse({ ok: false, status: 409, body: { detail: "active key already exists" } })
    );
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
    mockFetch.mockResolvedValueOnce(
      mockResponse({
        ok: false,
        status: 429,
        body: { detail: "regenerate cooldown active", extensions: { retryAfterMs: 60_000 } },
      })
    );
    const result = await regenerateKey();
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("cooldown");
      expect(result.error.retryAfterMs).toBe(60_000);
    }
  });
});
