import { getChangesPage } from "@/lib/api"
import { NextResponse } from "next/server"

export const dynamic   = "force-dynamic"
export const revalidate = 0

export async function GET(req: Request) {
  const url      = new URL(req.url)
  const provider = url.searchParams.get("provider") ?? undefined
  const since    = url.searchParams.get("since")    ?? undefined
  const before   = url.searchParams.get("before")   ?? undefined
  const limit    = url.searchParams.get("limit")
    ? Number(url.searchParams.get("limit"))
    : undefined

  try {
    const result = await getChangesPage({ provider, since, before, limit })
    return NextResponse.json(result)
  } catch (e) {
    const msg = e instanceof Error ? e.message : "Unknown error"
    return NextResponse.json({ error: msg }, { status: 500 })
  }
}
