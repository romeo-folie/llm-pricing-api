"use server"

import { getCompare } from "@/lib/api"

export interface CostResult {
  modelId:     string
  modelName:   string
  inputCost:   number
  outputCost:  number
  total:       number
}

export async function calculateCost(
  modelIds:    string[],
  inputTokens: number,
  outputTokens: number,
  period:      "daily" | "monthly" | "yearly",
): Promise<CostResult[]> {
  // Clamp inputs server-side — never trust client-supplied numbers
  const safeIds    = modelIds.slice(0, 5)  // mirror backend's documented compare limit
  const safeInput  = Math.max(0, Math.min(10_000_000_000, Math.round(inputTokens)))
  const safeOutput = Math.max(0, Math.min(10_000_000_000, Math.round(outputTokens)))

  const multiplier = period === "daily" ? 1 : period === "monthly" ? 30 : 365

  const models = await getCompare(safeIds)

  return models.map((m) => {
    const inputCost  = (safeInput  / 1_000_000) * m.input_price_per_m  * multiplier
    const outputCost = (safeOutput / 1_000_000) * m.output_price_per_m * multiplier
    return {
      modelId:    m.id,
      modelName:  m.name,
      inputCost,
      outputCost,
      total: inputCost + outputCost,
    }
  })
}
