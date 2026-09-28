import { beforeEach, describe, expect, it, vi } from "vitest"

const { getModelMock, getModelHistoryMock } = vi.hoisted(() => ({
  getModelMock: vi.fn(),
  getModelHistoryMock: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  getModel: getModelMock,
  getModelHistory: getModelHistoryMock,
}))

import { GET } from "@/app/api/model/[id]/route"

function call(url: string, id = "42") {
  return GET(new Request(url), { params: Promise.resolve({ id }) })
}

describe("GET /api/model/[id]", () => {
  beforeEach(() => {
    getModelMock.mockReset()
    getModelHistoryMock.mockReset()
  })

  it("ignores caller-supplied from/to so they never reach a cached fetch URL", async () => {
    getModelMock.mockResolvedValue({ id: "42" })
    getModelHistoryMock.mockResolvedValue([])
    await call("http://localhost/api/model/42?from=2026-01-01T00:00:01Z&to=2026-09-28T12:00:00Z")
    expect(getModelHistoryMock).toHaveBeenCalledWith("42")
  })

  it("returns a fixed error message, not the upstream detail", async () => {
    getModelMock.mockRejectedValue(new Error("API error 404 at /v1/models/42: internal row detail"))
    const res = await call("http://localhost/api/model/42")
    expect(res.status).toBe(404)
    const body = await res.json()
    expect(JSON.stringify(body)).not.toContain("/v1/models")
    expect(JSON.stringify(body)).not.toContain("internal row detail")
  })

  it("maps unexpected failures to 500 with a fixed message", async () => {
    getModelMock.mockRejectedValue(new Error("socket hang up"))
    const res = await call("http://localhost/api/model/42")
    expect(res.status).toBe(500)
    expect(JSON.stringify(await res.json())).not.toContain("socket")
  })
})
