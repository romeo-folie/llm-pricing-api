import { describe, expect, it } from "vitest"
import { decodeSlugParts } from "@/lib/routes"

describe("decodeSlugParts", () => {
  it("reconstructs nested model slugs", () => {
    expect(decodeSlugParts(["openai", "gpt-4o"])).toBe("openai/gpt-4o")
  })

  it("decodes encoded variant separators exactly once", () => {
    expect(decodeSlugParts(["openai", "gpt-4o%3Abatch"])).toBe("openai/gpt-4o:batch")
  })

  it("does not fail on a malformed encoded segment", () => {
    expect(decodeSlugParts(["provider", "model%zz"])).toBe("provider/model%zz")
  })
})
