/**
 * Agent device-grant client — typed wrappers around the /auth/agent/* endpoints.
 *
 * This backs the /activate approval screen. The flow it belongs to:
 *
 *   agent  → POST /auth/agent/device        (gets device_code + user_code)
 *   user   → opens /activate?code=USER_CODE (this client)
 *   user   → POST /auth/agent/approve       (consent, bound to their session)
 *   agent  → POST /auth/agent/token         (collects the key, once)
 *
 * The API key never passes through this screen. The user's only job is the
 * decision, and the terminal shows the same user code so the two can be
 * compared before consenting.
 */

export type AgentGrantStatus = "pending" | "approved" | "denied" | "redeemed" | "expired"

export type AgentGrant = {
  user_code: string
  /** Agent-supplied; render as text, never as markup. */
  client_name: string
  platform: string
  status: AgentGrantStatus
  expires_at: string
  /** The signed-in account that would be authorised. */
  email: string
}

export type AgentDecision = "approve" | "deny"

export type AgentDecisionResponse = {
  status: "approved" | "denied"
  client_name: string
}

export type AgentErrorCode =
  | "needs_session"
  | "unknown_code"
  | "expired"
  | "already_used"
  | "rate_limited"
  | "server"
  | "aborted"
  | "network"

export type AgentResult<T> =
  | { ok: true; data: T }
  | { ok: false; code: AgentErrorCode; message: string; retryAfterMs?: number }

// ── User code handling ────────────────────────────────────────────────────────

/**
 * Crockford base32, 8 characters. Mirrors signup.ValidUserCode on the server:
 * I, L, O and U are excluded so a code cannot be misread.
 */
const USER_CODE_RE = /^[0-9A-HJKMNP-TV-Z]{8}$/

/**
 * Canonicalise a user-typed code the same way the server does: strip
 * separators, upper-case, and fold the characters Crockford treats as aliases
 * (I and L to 1, O to 0).
 *
 * Doing this client-side means a simple typo is caught without a request, which
 * matters because the server deliberately does not confirm code validity to an
 * unauthenticated caller.
 */
export function normalizeUserCode(raw: string): string {
  let out = ""
  for (const ch of raw.trim().toUpperCase()) {
    if (ch === "-" || ch === " " || ch === "_" || ch === "\t") continue
    if (ch === "I" || ch === "L") out += "1"
    else if (ch === "O") out += "0"
    else out += ch
  }
  return out
}

/** Reports whether a normalised code is well formed. */
export function isValidUserCode(normalized: string): boolean {
  return USER_CODE_RE.test(normalized)
}

// ── Endpoints ─────────────────────────────────────────────────────────────────

/** Load the grant the user is being asked to approve. Requires a session. */
export async function getAgentGrant(
  userCode: string,
  signal?: AbortSignal
): Promise<AgentResult<AgentGrant>> {
  return request<AgentGrant>(
    `/auth/agent/grant?user_code=${encodeURIComponent(userCode)}`,
    { method: "GET", signal }
  )
}

/** Record the user's decision. Requires a session. */
export async function decideAgentGrant(
  userCode: string,
  decision: AgentDecision,
  signal?: AbortSignal
): Promise<AgentResult<AgentDecisionResponse>> {
  return request<AgentDecisionResponse>("/auth/agent/approve", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ user_code: userCode, decision }),
    signal,
  })
}

// ── Internals ─────────────────────────────────────────────────────────────────

async function request<T>(url: string, init: RequestInit): Promise<AgentResult<T>> {
  let res: Response
  try {
    res = await fetch(url, init)
  } catch (e) {
    if ((e as Error).name === "AbortError") {
      return { ok: false, code: "aborted", message: "Request was aborted" }
    }
    return {
      ok: false,
      code: "network",
      message: "Could not reach the server. Check your connection and try again.",
    }
  }

  if (res.ok) {
    try {
      return { ok: true, data: (await res.json()) as T }
    } catch {
      return { ok: false, code: "server", message: "The server returned an unreadable response." }
    }
  }

  const message = await problemDetail(res)
  switch (res.status) {
    case 401:
      return { ok: false, code: "needs_session", message: "Sign in to review this request." }
    case 404:
      return { ok: false, code: "unknown_code", message }
    case 410:
      return { ok: false, code: "expired", message }
    case 429:
      return {
        ok: false,
        code: "rate_limited",
        message,
        retryAfterMs: parseRetryAfter(res),
      }
    default:
      return { ok: false, code: "server", message }
  }
}

/** Reads the RFC 7807 `detail` field, falling back to a safe generic string. */
async function problemDetail(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { detail?: unknown; error?: unknown }
    if (typeof body.detail === "string" && body.detail !== "") return body.detail
    if (typeof body.error === "string" && body.error !== "") return body.error
  } catch {
    // Non-JSON error body; fall through to the generic message.
  }
  return "Something went wrong. Please try again."
}

function parseRetryAfter(res: Response): number | undefined {
  const raw = res.headers.get("Retry-After")
  if (!raw) return undefined
  const seconds = parseInt(raw, 10)
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : undefined
}
