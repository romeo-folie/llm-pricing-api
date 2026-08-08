import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Model } from "@/lib/api"

const { getCompareMock } = vi.hoisted(() => ({
  getCompareMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getCompare: getCompareMock,
}))

import { calculateCost } from "@/app/calculator/actions"

const model: Model = {
  id: "201675",
  name: "nemotron-3-super-120b-a12b",
  slug: "nvidia/nemotron-3-super-120b-a12b",
  provider: "nvidia",
  modality: "text",
  context_window: 1_000_000,
  input_price_per_m: 0.085,
  output_price_per_m: 0.4,
  updated_at: "2026-08-08T02:51:26Z",
  underlying_provider: null,
  trust: {
    confirmed_at: "2026-08-08T02:51:26Z",
    source: "openrouter",
    confidence: "medium",
    age_hours: 1,
    change_velocity: 0,
  },
}

describe("calculateCost", () => {
  beforeEach(() => {
    getCompareMock.mockReset()
  })

  it("forwards URL model slugs and calculates monthly costs", async () => {
    getCompareMock.mockResolvedValue([model])

    const result = await calculateCost(
      ["nvidia/nemotron-3-super-120b-a12b"],
      100_000,
      10_000,
      "monthly",
    )

    expect(getCompareMock).toHaveBeenCalledWith([
      "nvidia/nemotron-3-super-120b-a12b",
    ])
    expect(result).toHaveLength(1)
    expect(result[0].modelId).toBe("201675")
    expect(result[0].modelName).toBe("nemotron-3-super-120b-a12b")
    expect(result[0].inputCost).toBeCloseTo(0.255)
    expect(result[0].outputCost).toBeCloseTo(0.12)
    expect(result[0].total).toBeCloseTo(0.375)
  })
})
