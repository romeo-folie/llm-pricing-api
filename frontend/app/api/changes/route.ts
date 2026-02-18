import { getChanges } from "@/lib/api"
import { NextResponse } from "next/server"

export const dynamic   = "force-dynamic"
export const revalidate = 0

export async function GET(req: Request) {
  const url      = new URL(req.url)
  const provider = url.searchParams.get("provider") ?? undefined
  const since    = url.searchParams.get("since")    ?? undefined

  const changes = await getChanges({ provider, since })
  return NextResponse.json(changes)
}
