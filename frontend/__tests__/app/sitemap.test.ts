import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Model } from "@/lib/api"

const { getModelsPaginatedMock, getProvidersMock } = vi.hoisted(() => ({
  getModelsPaginatedMock: vi.fn(),
  getProvidersMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModelsPaginated: getModelsPaginatedMock,
  getProviders: getProvidersMock,
}))

import sitemap from "@/app/sitemap"

function model(overrides: Partial<Model>): Model {
  return {
    id: "1",
    name: "Model",
    slug: "provider/model",
    provider: "provider",
    modality: "text",
    context_window: 128_000,
    input_price_per_m: 1,
    output_price_per_m: 2,
    updated_at: new Date().toISOString(),
    underlying_provider: null,
    trust: {
      confirmed_at: new Date().toISOString(),
      source: "provider_docs",
      confidence: "high",
      age_hours: 1,
      change_velocity: 0,
    },
    ...overrides,
  }
}

describe("sitemap comparison URLs", () => {
  beforeEach(() => {
    getModelsPaginatedMock.mockReset()
    getProvidersMock.mockReset()
  })

  it("publishes comparison URLs in canonical order regardless of API order", async () => {
    getModelsPaginatedMock.mockResolvedValue({
      data: [
        model({
          id: "1",
          slug: "openai/gpt-4o",
          provider: "openai",
        }),
        model({
          id: "2",
          slug: "anthropic/claude-sonnet-4",
          provider: "anthropic",
        }),
        model({
          id: "3",
          slug: "fireworks_ai/accounts/fireworks/models/ssd-1b",
          provider: "fireworks_ai",
        }),
      ],
      total: 3,
    })
    getProvidersMock.mockResolvedValue([])

    const entries = await sitemap()

    expect(entries.map((entry) => entry.url)).toContain(
      "https://llmrates.live/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    )
    expect(entries.map((entry) => entry.url)).not.toContain(
      "https://llmrates.live/compare/openai/gpt-4o-vs-anthropic/claude-sonnet-4",
    )
    expect(entries.map((entry) => entry.url)).not.toContain(
      "https://llmrates.live/models/fireworks_ai/accounts/fireworks/models/ssd-1b",
    )
  })
})
