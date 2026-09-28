import { getModel, getModelHistory } from "@/lib/api"
import { NextResponse } from "next/server"

export async function GET(
  _req: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params

  try {
    const model = await getModel(id)
    // Use numeric model.id for the history endpoint (integer-only backend route).
    // No caller-supplied from/to: they would be part of the cached fetch URL.
    const history = await getModelHistory(model.id).catch(() => [])
    return NextResponse.json({ model, history })
  } catch (e) {
    const msg    = e instanceof Error ? e.message : ""
    const parsed = msg.match(/^API error (\d{3}) at /)
    const status = parsed ? parseInt(parsed[1], 10) : 500
    console.error(`[api/model] failed to load model ${id}:`, msg)
    return NextResponse.json(
      { error: status === 404 ? "Model not found" : "Failed to load model" },
      { status },
    )
  }
}
