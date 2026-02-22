import { NextResponse } from "next/server";
import {
  signSession,
  cookieName,
  cookieOptions,
  type AccountSession,
} from "@/lib/session";

export const dynamic = "force-dynamic";

const API_BASE = process.env.LLM_PRICING_API_BASE_URL || "http://localhost:8080";

/**
 * POST /api/account/verify
 *
 * Accepts { key } in the request body, forwards it to the Go API for Unkey
 * verification, and on success sets a signed HttpOnly session cookie containing
 * the account data. The raw key never reaches the client after this point.
 */
export async function POST(req: Request) {
  let key: string | undefined;
  try {
    const body = await req.json();
    key = typeof body?.key === "string" ? body.key.trim() : undefined;
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 });
  }

  if (!key) {
    return NextResponse.json({ error: "key is required" }, { status: 400 });
  }

  let goRes: Response;
  try {
    goRes = await fetch(`${API_BASE}/api/account/verify`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key }),
    });
  } catch (e) {
    const msg = e instanceof Error ? e.message : "Unknown error";
    return NextResponse.json({ error: `Upstream error: ${msg}` }, { status: 502 });
  }

  if (!goRes.ok) {
    const detail = await goRes.text().catch(() => "");
    return NextResponse.json(
      { error: "Key verification failed", detail },
      { status: goRes.status === 401 ? 401 : 502 }
    );
  }

  const data: AccountSession = await goRes.json();

  // Sign and set the session cookie. The raw key value is never stored.
  const cookieValue = signSession(data);
  const response = NextResponse.json({ ok: true, tier: data.tier });
  response.headers.set(
    "Set-Cookie",
    `${cookieName()}=${cookieValue}; ${cookieOptions()}`
  );
  return response;
}
