import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Model } from "@/lib/api"

const { getModelMock, permanentRedirectMock } = vi.hoisted(() => ({
  getModelMock: vi.fn(),
  permanentRedirectMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModel: getModelMock,
}))

vi.mock("next/navigation", () => ({
  notFound: vi.fn(),
  permanentRedirect: permanentRedirectMock,
}))

import ComparePage, { generateMetadata } from "@/app/compare/[...slug]/page"

const anthropic: Model = {
  id: "1",
  name: "Claude Sonnet 4",
  slug: "anthropic/claude-sonnet-4",
  provider: "anthropic",
  modality: "text",
  context_window: 200_000,
  input_price_per_m: 3,
  output_price_per_m: 15,
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

const openai: Model = {
  ...anthropic,
  id: "2",
  name: "GPT-4o",
  slug: "openai/gpt-4o",
  provider: "openai",
}

const reverseParams = Promise.resolve({
  slug: ["openai", "gpt-4o-vs-anthropic", "claude-sonnet-4"],
})

const canonicalParams = Promise.resolve({
  slug: ["anthropic", "claude-sonnet-4-vs-openai", "gpt-4o"],
})

describe("comparison canonicalization", () => {
  beforeEach(() => {
    getModelMock.mockReset()
    permanentRedirectMock.mockReset()
    getModelMock.mockImplementation(async (slug: string) =>
      slug === openai.slug ? openai : slug === anthropic.slug ? anthropic : null,
    )
  })

  it("declares the canonical order even when the reverse URL is inspected", async () => {
    const metadata = await generateMetadata({ params: reverseParams })

    expect(metadata.alternates).toEqual({
      canonical: "/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    })
  })

  it("permanently redirects a reverse-order comparison URL", async () => {
    permanentRedirectMock.mockImplementation((path: string) => {
      throw new Error(`redirect:${path}`)
    })

    await expect(ComparePage({ params: reverseParams })).rejects.toThrow(
      "redirect:/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    )
    expect(permanentRedirectMock).toHaveBeenCalledWith(
      "/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    )
  })

  it("does not redirect the canonical comparison URL", async () => {
    await ComparePage({ params: canonicalParams })

    expect(permanentRedirectMock).not.toHaveBeenCalled()
  })
})
