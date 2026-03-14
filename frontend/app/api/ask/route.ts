import { NextRequest, NextResponse } from "next/server";

const API_BASE =
  process.env.LLM_PRICING_API_BASE_URL || "https://api.llmrates.live";

// Set LLM_PRICING_DEMO_KEY in your .env.local / Railway env to enable live
// /v1/ask responses on the landing page demo widget. Without it, the widget
// returns { simulated: true } and falls back to preset responses client-side.
const DEMO_KEY = process.env.LLM_PRICING_DEMO_KEY;

export async function POST(req: NextRequest) {
  if (!DEMO_KEY) {
    return NextResponse.json({ simulated: true }, { status: 200 });
  }

  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid json" }, { status: 400 });
  }

  const upstream = await fetch(`${API_BASE}/v1/ask`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${DEMO_KEY}`,
    },
    body: JSON.stringify(body),
  });

  const data = await upstream.json();
  return NextResponse.json(data, { status: upstream.status });
}
