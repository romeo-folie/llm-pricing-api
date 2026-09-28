import { describe, expect, it } from "vitest"
import { decodeCachePayload, encodeCachePayload } from "@/lib/cache-codec"

describe("cache codec", () => {
  it("round-trips structured data", () => {
    const value = [
      { id: "1", name: "GPT-4o", trust: { confidence: "high", age_hours: 2.5 }, underlying_provider: null },
      { id: "2", name: "Claude", trust: { confidence: "low", age_hours: 0 }, underlying_provider: "bedrock" },
    ]
    expect(decodeCachePayload(encodeCachePayload(value))).toEqual(value)
  })

  it("round-trips an empty list", () => {
    expect(decodeCachePayload(encodeCachePayload([]))).toEqual([])
  })

  it("preserves non-ASCII text", () => {
    const value = [{ name: "Modèle 日本語 🚀" }]
    expect(decodeCachePayload(encodeCachePayload(value))).toEqual(value)
  })

  it("keeps a catalog-sized payload well under the 2MB data-cache limit", () => {
    const models = Array.from({ length: 6000 }, (_, i) => ({
      id: String(i),
      name: `provider/model-${i}`,
      slug: `provider/model-${i}`,
      provider: "provider",
      modality: "text",
      context_window: 128000,
      input_price_per_m: 2.5,
      output_price_per_m: 10,
      updated_at: "2026-09-01T00:00:00Z",
      underlying_provider: null,
      trust: {
        confirmed_at: "2026-09-01T00:00:00Z",
        source: "openrouter",
        confidence: "high",
        age_hours: 2,
        change_velocity: 0,
      },
    }))
    const raw = JSON.stringify(models).length
    const encoded = encodeCachePayload(models)
    expect(raw).toBeGreaterThan(2 * 1024 * 1024)
    // Next.js measures JSON.stringify(entry).length against its 2MB cap.
    expect(JSON.stringify(encoded).length).toBeLessThan(512 * 1024)
  })

  it("rejects a corrupted payload instead of returning garbage", () => {
    expect(() => decodeCachePayload("not-gzip")).toThrow()
  })
})
