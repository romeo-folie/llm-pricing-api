/**
 * Signup API client — typed wrappers around the backend /auth/signup/* endpoints.
 */

export type SignupStep =
  | "request"        // entry: email form
  | "sent"           // link sent, waiting for user to check email
  | "verifying"      // /verify?token=... being processed
  | "verified"       // token consumed, session established
  | "issued"         // key revealed for first time
  | "already-issued" // user has existing key
  | "error";         // unrecoverable error

export type ApiError = {
  code: "rate_limited" | "cooldown" | "invalid_email" | "disposable_domain" | "unknown";
  message: string;
  retryAfterMs?: number;
};

export type IdentityResponse = {
  id: string;
  email: string;
  email_verified: boolean;
  has_active_key: boolean;
};

export type KeyIssueResponse = {
  plaintext: string;
  provider_key_id: string;
};

// ── Request link ──────────────────────────────────────────────────────────────

export async function requestMagicLink(
  email: string,
  signal?: AbortSignal
): Promise<{ ok: true } | { ok: false; error: ApiError }> {
  try {
    const res = await fetch("/auth/signup/request-link", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email }),
      signal,
    });
    if (res.ok) return { ok: true };

    const body = await res.json().catch(() => ({}));
    return {
      ok: false,
      error: parseError(res, body),
    };
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    return { ok: false, error: { code: "unknown", message: "Network error" } };
  }
}

// ── Verify token (server-side redirect; client calls /auth/signup/me after) ───

export async function getIdentity(
  signal?: AbortSignal
): Promise<{ ok: true; identity: IdentityResponse } | { ok: false; error: ApiError }> {
  try {
    const res = await fetch("/auth/signup/me", { signal });
    if (res.ok) {
      const identity = await res.json();
      return { ok: true, identity };
    }
    if (res.status === 401) {
      return { ok: false, error: { code: "unknown", message: "Session not found" } };
    }
    return { ok: false, error: { code: "unknown", message: "Failed to load identity" } };
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    return { ok: false, error: { code: "unknown", message: "Network error" } };
  }
}

// ── Issue key ─────────────────────────────────────────────────────────────────

export async function issueKey(
  signal?: AbortSignal
): Promise<{ ok: true; key: KeyIssueResponse } | { ok: false; error: ApiError }> {
  try {
    const res = await fetch("/auth/signup/issue-key", {
      method: "POST",
      signal,
    });
    if (res.ok) {
      const key = await res.json();
      return { ok: true, key };
    }
    const body = await res.json().catch(() => ({}));
    return { ok: false, error: parseError(res, body) };
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    return { ok: false, error: { code: "unknown", message: "Network error" } };
  }
}

// ── Regenerate key ────────────────────────────────────────────────────────────

export async function regenerateKey(
  signal?: AbortSignal
): Promise<{ ok: true; key: KeyIssueResponse } | { ok: false; error: ApiError }> {
  try {
    const res = await fetch("/auth/signup/regenerate-key", {
      method: "POST",
      signal,
    });
    if (res.ok) {
      const key = await res.json();
      return { ok: true, key };
    }
    const body = await res.json().catch(() => ({}));
    return { ok: false, error: parseError(res, body) };
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    return { ok: false, error: { code: "unknown", message: "Network error" } };
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Parse an error response using RFC 7807 ProblemDetail format.
 * Reads `body.detail` as the primary message and `body.extensions` for extra
 * fields like `retryAfterMs`. Falls back to `body.error` for legacy payloads.
 * For 429 responses, also reads the standard `Retry-After` header (in seconds).
 */
function parseError(res: Response, body: Record<string, unknown>): ApiError {
  const status = res.status;
  const extensions = (body.extensions ?? {}) as Record<string, unknown>;

  // Primary message: RFC7807 `detail`, fallback to legacy `error` field.
  const detailMsg =
    typeof body.detail === "string"
      ? body.detail
      : typeof body.error === "string"
        ? body.error
        : undefined;

  if (status === 429) {
    // Extensions may carry retryAfterMs directly.
    const extRetryMs = Number(extensions.retryAfterMs ?? 0);
    // Legacy body fields.
    const bodyRetryMs = Number(body.retry_after_ms ?? 0);
    const bodyRetrySec = Number(body.retry_after ?? 0);
    // Standard Retry-After header (in seconds).
    const headerRetrySec = Number(res.headers.get("Retry-After") ?? 0);
    const retryMs = extRetryMs || bodyRetryMs || bodyRetrySec * 1000 || headerRetrySec * 1000;

    const msg = detailMsg ?? "Too many requests";
    if (msg.toLowerCase().includes("cooldown")) {
      return { code: "cooldown", message: msg, retryAfterMs: retryMs };
    }
    return { code: "rate_limited", message: msg, retryAfterMs: retryMs };
  }
  if (status === 422 || status === 400) {
    const msg = detailMsg ?? "Invalid request";
    if (msg.toLowerCase().includes("disposable")) {
      return { code: "disposable_domain", message: msg };
    }
    return { code: "invalid_email", message: msg };
  }
  return {
    code: "unknown",
    message: detailMsg ?? "Something went wrong",
  };
}
