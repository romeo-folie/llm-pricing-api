import { describe, expect, it } from "vitest"
import {
  canonicalComparePath,
  decodeSlugParts,
  encodeSlugPath,
} from "@/lib/routes"

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

describe("encodeSlugPath", () => {
  it("preserves route separators while encoding individual segments", () => {
    expect(encodeSlugPath("azure_ai/FW-GLM-5")).toBe("azure_ai/FW-GLM-5")
  })
})

describe("canonicalComparePath", () => {
  it("uses a stable order for the same two model slugs", () => {
    expect(canonicalComparePath("openai/gpt-4o", "anthropic/claude-sonnet-4")).toBe(
      "/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    )
    expect(canonicalComparePath("anthropic/claude-sonnet-4", "openai/gpt-4o")).toBe(
      "/compare/anthropic/claude-sonnet-4-vs-openai/gpt-4o",
    )
  })
})
