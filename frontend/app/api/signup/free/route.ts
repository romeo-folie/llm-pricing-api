import { NextResponse } from "next/server"

export const dynamic = "force-dynamic"

const API_BASE = process.env.LLM_PRICING_API_BASE_URL || "http://localhost:8080"

/**
 * POST /api/signup/free
 *
 * Proxies the email signup request to the Go API, keeping the upstream
 * URL server-side and avoiding CORS issues from the browser.
 */
export async function POST(req: Request) {
  let email: string | undefined
  try {
    const body = await req.json()
    email = typeof body?.email === "string" ? body.email.trim() : undefined
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  if (!email) {
    return NextResponse.json({ error: "email is required" }, { status: 400 })
  }

  let goRes: Response
  try {
    goRes = await fetch(`${API_BASE}/api/v1/signup/free`, {
      method:  "POST",
      headers: { "Content-Type": "application/json" },
      body:    JSON.stringify({ email }),
    })
  } catch (e) {
    const msg = e instanceof Error ? e.message : "Unknown error"
    return NextResponse.json({ error: `Upstream error: ${msg}` }, { status: 502 })
  }

  // Pass through the upstream status code verbatim (including 429 rate limit).
  if (!goRes.ok) {
    const detail = await goRes.text().catch(() => "")
    return NextResponse.json(
      { error: "Signup failed", detail },
      { status: goRes.status }
    )
  }

  return NextResponse.json({ ok: true }, { status: 200 })
}
