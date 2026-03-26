import { NextRequest, NextResponse } from "next/server";

const API_BASE =
  process.env.LLM_PRICING_API_BASE_URL || "https://api.llmrates.live";
const API_KEY = process.env.LLM_PRICING_API_KEY;

export async function POST(req: NextRequest) {
  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json(
      { error: "invalid JSON body" },
      { status: 400 }
    );
  }

  const upstream = await fetch(`${API_BASE}/v1/feedback`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(API_KEY ? { Authorization: `Bearer ${API_KEY}` } : {}),
    },
    body: JSON.stringify(body),
    cache: "no-store",
  });

  const contentType = upstream.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    const text = await upstream.text();
    return NextResponse.json(
      { error: "upstream returned non-JSON response", detail: text.slice(0, 200) },
      { status: upstream.status }
    );
  }

  let data: unknown;
  try {
    data = await upstream.json();
  } catch {
    return NextResponse.json(
      { error: "failed to parse upstream response" },
      { status: 502 }
    );
  }

  return NextResponse.json(data, { status: upstream.status });
}
