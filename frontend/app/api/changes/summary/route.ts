import { getChangesSummary } from "@/lib/api"
import { NextResponse } from "next/server"

export const dynamic   = "force-dynamic"
export const revalidate = 0

export async function GET(req: Request) {
  const url      = new URL(req.url)
  const window   = url.searchParams.get("window")   ?? undefined
  const since    = url.searchParams.get("since")     ?? undefined
  const provider = url.searchParams.get("provider")  ?? undefined

  try {
    const summary = await getChangesSummary({ window, since, provider })
    return NextResponse.json(summary)
  } catch (e) {
    const msg = e instanceof Error ? e.message : "Unknown error"
    return NextResponse.json({ error: msg }, { status: 500 })
  }
}
