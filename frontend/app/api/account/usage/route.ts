import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import {
  verifySession,
  makeAccountToken,
  cookieName,
} from "@/lib/session";

export const dynamic = "force-dynamic";

const API_BASE = process.env.LLM_PRICING_API_BASE_URL || "http://localhost:8080";

/**
 * GET /api/account/usage
 *
 * Reads the session cookie, extracts the keyId, and proxies a request to the
 * Go API to fetch today's usage via Unkey's GetVerifications (which does NOT
 * consume the user's rate limit). Returns { usageToday, dailyLimit }.
 */
export async function GET() {
  const jar = await cookies();
  const cookie = jar.get(cookieName());
  if (!cookie?.value) {
    return NextResponse.json({ error: "Not authenticated" }, { status: 401 });
  }

  const session = verifySession(cookie.value);
  if (!session) {
    return NextResponse.json({ error: "Invalid session" }, { status: 401 });
  }

  const { keyId } = session;
  const token = makeAccountToken(keyId);

  let goRes: Response;
  try {
    goRes = await fetch(
      `${API_BASE}/api/account/usage?key_id=${encodeURIComponent(keyId)}`,
      {
        headers: { "X-Account-Token": token },
        // Don't cache — usage is live data.
        cache: "no-store",
      }
    );
  } catch (e) {
    const msg = e instanceof Error ? e.message : "Unknown error";
    return NextResponse.json({ error: `Upstream error: ${msg}` }, { status: 502 });
  }

  if (!goRes.ok) {
    const detail = await goRes.text().catch(() => "");
    return NextResponse.json({ error: "Failed to fetch usage", detail }, { status: 502 });
  }

  const data = await goRes.json();
  return NextResponse.json(data);
}
