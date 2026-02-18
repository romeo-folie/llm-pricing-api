import { getModel, getModelHistory } from "@/lib/api"
import { NextResponse } from "next/server"

export async function GET(
  _req: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params
  const url = new URL(_req.url)
  const from = url.searchParams.get("from") ?? undefined
  const to   = url.searchParams.get("to")   ?? undefined

  const [model, history] = await Promise.all([
    getModel(id),
    getModelHistory(id, from, to),
  ])

  return NextResponse.json({ model, history })
}
