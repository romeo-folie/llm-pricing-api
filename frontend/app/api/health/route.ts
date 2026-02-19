import { NextResponse } from "next/server"

// Lightweight health endpoint used by Railway's healthcheck probe.
// Returns 200 immediately without triggering SSR or data fetching.
export function GET() {
  return NextResponse.json({ status: "ok" }, { status: 200 })
}
