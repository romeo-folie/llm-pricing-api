import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Model } from "@/lib/api"

const { getModelMock } = vi.hoisted(() => ({
  getModelMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModel: getModelMock,
}))

import { generateMetadata } from "@/app/models/[...slug]/page"

const model: Model = {
  id: "1",
  name: "GPT-4o",
  slug: "openai/gpt-4o",
  provider: "openai",
  modality: "text",
  context_window: 128_000,
  input_price_per_m: 2.5,
  output_price_per_m: 10,
  updated_at: "2026-08-20T08:00:00Z",
  underlying_provider: null,
  trust: {
    confirmed_at: "2026-08-20T08:00:00Z",
    source: "provider_docs",
    confidence: "high",
    age_hours: 1,
    change_velocity: 0,
  },
}

describe("model detail metadata", () => {
  beforeEach(() => {
    getModelMock.mockReset()
  })

  it("keeps canonical provider/model pages indexable", async () => {
    getModelMock.mockResolvedValue(model)

    const metadata = await generateMetadata({
      params: Promise.resolve({ slug: ["openai", "gpt-4o"] }),
    })

    expect(metadata.robots).toBeUndefined()
    expect(metadata.alternates).toEqual({ canonical: "/models/openai/gpt-4o" })
  })

  it("noindexes raw deep resource paths while preserving their product URL", async () => {
    const rawModel = {
      ...model,
      name: "accounts/fireworks/models/SSD-1B",
      slug: "fireworks_ai/accounts/fireworks/models/SSD-1B",
      provider: "fireworks_ai",
    }
    getModelMock.mockResolvedValue(rawModel)

    const metadata = await generateMetadata({
      params: Promise.resolve({
        slug: ["fireworks_ai", "accounts", "fireworks", "models", "SSD-1B"],
      }),
    })

    expect(metadata.robots).toEqual({ index: false, follow: true })
    expect(metadata.alternates).toEqual({
      canonical: "/models/fireworks_ai/accounts/fireworks/models/SSD-1B",
    })
  })

  it("noindexes bare upstream identifiers", async () => {
    const rawModel = {
      ...model,
      name: "eu.anthropic.claude-sonnet-5",
      slug: "eu.anthropic.claude-sonnet-5",
      provider: "anthropic",
    }
    getModelMock.mockResolvedValue(rawModel)

    const metadata = await generateMetadata({
      params: Promise.resolve({ slug: ["eu.anthropic.claude-sonnet-5"] }),
    })

    expect(metadata.robots).toEqual({ index: false, follow: true })
    expect(metadata.alternates).toEqual({
      canonical: "/models/eu.anthropic.claude-sonnet-5",
    })
  })
})
