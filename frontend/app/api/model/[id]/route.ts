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
    // Fetch model and history independently — history is Dev+ tier gated and
    // may return 403 on Free-tier keys. The modal should still work without it.
    const model = await getModel(id)
    // Use numeric model.id for the history endpoint (integer-only backend route)
    const history = await getModelHistory(model.id, from, to).catch(() => [])
    return NextResponse.json({ model, history })
  } catch (e) {
    const msg    = e instanceof Error ? e.message : "Unknown error"
    const parsed = msg.match(/^API error (\d{3}) at /)
    const status = parsed ? parseInt(parsed[1], 10) : 500
    return NextResponse.json({ error: msg }, { status })
  }
}
