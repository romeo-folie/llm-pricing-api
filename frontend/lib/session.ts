import { createHmac, timingSafeEqual } from "crypto";

// Fail-fast in production if SESSION_SECRET is not set. An empty secret causes
// HMAC-SHA256 to use "" as the key, making cookie signatures guessable.
// The Go API enforces the same check at startup via config.Load().
if (!process.env.SESSION_SECRET && process.env.NODE_ENV === "production") {
  throw new Error("SESSION_SECRET environment variable must be set in production");
}
const SESSION_SECRET = process.env.SESSION_SECRET ?? "";

const COOKIE_NAME = "account_session";
const COOKIE_MAX_AGE = 60 * 60; // 1 hour in seconds

export interface AccountSession {
  tier: string;
  maskedKey: string;
  keyId: string;
  dailyLimit: number;
  usageToday: number;
  endsAt?: string;
  portalUrl?: string;
}

// --- cookie signing ---

function hmacSign(data: string, secret: string): string {
  return createHmac("sha256", secret).update(data).digest("hex");
}

/** Encode a session payload into a signed cookie value. */
export function signSession(session: AccountSession): string {
  const payload = Buffer.from(JSON.stringify(session)).toString("base64url");
  const sig = hmacSign(payload, SESSION_SECRET);
  return `${payload}.${sig}`;
}

/** Decode and verify a signed cookie value. Returns null if invalid. */
export function verifySession(cookie: string): AccountSession | null {
  const dot = cookie.lastIndexOf(".");
  if (dot === -1) return null;

  const payload = cookie.slice(0, dot);
  const sig = cookie.slice(dot + 1);
  const expected = hmacSign(payload, SESSION_SECRET);

  // Constant-time comparison using Node's crypto.timingSafeEqual to prevent
  // timing side-channel attacks. Both inputs must be same-length Buffers;
  // the length guard avoids the RangeError timingSafeEqual throws on mismatch.
  const sigBuf = Buffer.from(sig);
  const expectedBuf = Buffer.from(expected);
  if (sigBuf.length !== expectedBuf.length) return null;
  if (!timingSafeEqual(sigBuf, expectedBuf)) return null;

  try {
    return JSON.parse(Buffer.from(payload, "base64url").toString()) as AccountSession;
  } catch {
    return null;
  }
}

/** Build the X-Account-Token header value sent from Next.js → Go API. */
export function makeAccountToken(keyId: string): string {
  return hmacSign(keyId, SESSION_SECRET);
}

// --- cookie helpers ---

export function cookieName(): string {
  return COOKIE_NAME;
}

export function cookieOptions(): string {
  const secure = process.env.NODE_ENV === "production" ? "; Secure" : "";
  return `HttpOnly; SameSite=Lax; Max-Age=${COOKIE_MAX_AGE}; Path=/${secure}`;
}
