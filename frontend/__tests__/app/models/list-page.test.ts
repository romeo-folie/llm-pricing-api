import { beforeEach, describe, expect, it, vi } from "vitest"

const { getModelsMock, getProvidersMock } = vi.hoisted(() => ({
  getModelsMock: vi.fn(),
  getProvidersMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModels: getModelsMock,
  getProviders: getProvidersMock,
}))

import ModelsPage from "@/app/models/page"

function render(searchParams: Record<string, string>) {
  return ModelsPage({ searchParams: Promise.resolve(searchParams) })
}

describe("/models data loading", () => {
  beforeEach(() => {
    getModelsMock.mockReset().mockResolvedValue([])
    getProvidersMock.mockReset().mockResolvedValue([
      { id: "openai", name: "openai", model_count: 1 },
    ])
  })

  it("passes the canonical provider id for a known provider", async () => {
    await render({ provider: "OpenAI", q: "gpt" })
    expect(getModelsMock).toHaveBeenCalledWith(
      expect.objectContaining({ provider: "openai", q: "gpt" }),
    )
  })

  it("drops unknown or wildcard providers so they never reach the cache key", async () => {
    for (const provider of ["zz1", "o_enai", "%"]) {
      getModelsMock.mockClear()
      await render({ provider })
      expect(getModelsMock).toHaveBeenCalledWith(expect.objectContaining({ provider: undefined }))
    }
  })

  it("drops min_context and modality values the UI cannot produce", async () => {
    await render({ min_context: "12345", modality: "video" })
    expect(getModelsMock).toHaveBeenCalledWith(
      expect.objectContaining({ min_context: undefined, modality: undefined }),
    )
  })

  it("still loads models when the provider list fails, ignoring the provider filter", async () => {
    getProvidersMock.mockRejectedValue(new Error("down"))
    await render({ provider: "openai" })
    expect(getModelsMock).toHaveBeenCalledWith(expect.objectContaining({ provider: undefined }))
  })

  it("does not wait for the provider list when no provider filter is set", async () => {
    let release: (v: unknown) => void = () => {}
    getProvidersMock.mockReturnValue(new Promise((r) => { release = r }))
    const pending = render({ q: "gpt" })
    await new Promise((r) => setTimeout(r, 0))
    expect(getModelsMock).toHaveBeenCalled()
    release([])
    await pending
  })

  it("waits for the provider list before loading a provider-filtered catalog", async () => {
    let release: (v: unknown) => void = () => {}
    getProvidersMock.mockReturnValue(new Promise((r) => { release = r }))
    const pending = render({ provider: "openai" })
    await new Promise((r) => setTimeout(r, 0))
    expect(getModelsMock).not.toHaveBeenCalled()
    release([{ id: "openai", name: "openai", model_count: 1 }])
    await pending
    expect(getModelsMock).toHaveBeenCalledWith(expect.objectContaining({ provider: "openai" }))
  })
})
