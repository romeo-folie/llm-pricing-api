import { getModel, getModelHistory } from "@/lib/api"
import { NextResponse } from "next/server"

export async function GET(
  _req: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params
  const url  = new URL(_req.url)
  const from = url.searchParams.get("from") ?? undefined
  const to   = url.searchParams.get("to")   ?? undefined

  try {
    const [model, history] = await Promise.all([
      getModel(id),
      getModelHistory(id, from, to),
    ])
    return NextResponse.json({ model, history })
  } catch (e) {
    const msg    = e instanceof Error ? e.message : "Unknown error"
    const status = msg.includes("404") ? 404 : 500
    return NextResponse.json({ error: msg }, { status })
  }
}
