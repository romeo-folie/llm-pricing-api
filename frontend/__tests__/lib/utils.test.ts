import { describe, it, expect } from "vitest"
import { safeJsonLd } from "@/lib/utils"

describe("safeJsonLd", () => {
  it("escapes < and > characters to prevent script injection", () => {
    const result = safeJsonLd({ name: "<script>alert(1)</script>" })
    expect(result).not.toContain("<")
    expect(result).not.toContain(">")
    expect(result).toContain("\\u003c")
    expect(result).toContain("\\u003e")
  })

  it("escapes & characters", () => {
    const result = safeJsonLd({ name: "Tom & Jerry" })
    expect(result).not.toContain("&")
    expect(result).toContain("\\u0026")
  })

  it("produces valid JSON structure", () => {
    const input = { "@type": "Product", name: "Test", price: 1.5 }
    const result = safeJsonLd(input)
    // After unescaping the safe characters, it should parse back
    const unescaped = result
      .replace(/\\u003c/g, "<")
      .replace(/\\u003e/g, ">")
      .replace(/\\u0026/g, "&")
    expect(JSON.parse(unescaped)).toEqual(input)
  })

  it("handles nested objects", () => {
    const input = {
      "@context": "https://schema.org",
      brand: { "@type": "Brand", name: "OpenAI" },
    }
    const result = safeJsonLd(input)
    expect(result).toContain("schema.org")
    expect(result).toContain("OpenAI")
  })

  it("handles empty object", () => {
    expect(safeJsonLd({})).toBe("{}")
  })
})
