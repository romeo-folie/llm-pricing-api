import { beforeEach, describe, expect, it, vi } from "vitest"

const { getModelsMock, getProvidersMock } = vi.hoisted(() => ({
  getModelsMock: vi.fn(),
  getProvidersMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModels: getModelsMock,
  getProviders: getProvidersMock,
}))

import ProviderPage from "@/app/providers/[slug]/page"

function render(slug: string) {
  return ProviderPage({ params: Promise.resolve({ slug }) })
}

describe("/providers/[slug]", () => {
  beforeEach(() => {
    getModelsMock.mockReset().mockResolvedValue([])
    getProvidersMock.mockReset().mockResolvedValue([
      { id: "openai", name: "openai", model_count: 1 },
    ])
  })

  it("404s an unknown or wildcard slug before touching the model cache", async () => {
    for (const slug of ["%", "o_enai", "nope"]) {
      await expect(render(slug)).rejects.toMatchObject({
        digest: expect.stringContaining("404"),
      })
    }
    expect(getModelsMock).not.toHaveBeenCalled()
  })

  it("loads models for the canonical provider id", async () => {
    await render("OpenAI")
    expect(getModelsMock).toHaveBeenCalledWith({ provider: "openai" })
  })

  it("fails loudly rather than 404ing when the provider list is unavailable", async () => {
    getProvidersMock.mockRejectedValue(new Error("down"))
    await expect(render("openai")).rejects.toThrow("down")
    expect(getModelsMock).not.toHaveBeenCalled()
  })
})
