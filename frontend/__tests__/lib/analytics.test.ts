import { describe, it, expect } from "vitest"
import { analyticsPath } from "@/lib/analytics"

/**
 * The approval code is a live credential: it authorises a decision on a device
 * grant. It must never reach an analytics provider, so these tests pin the
 * redaction rather than trusting the call site.
 */
describe("analyticsPath", () => {
  it("strips the device-approval code", () => {
    expect(analyticsPath("/activate", "?code=ACDF-2345")).toBe("/activate")
  })

  it("strips a magic-link token", () => {
    expect(analyticsPath("/signup/verify", "?token=abc123")).toBe("/signup/verify")
  })

  it("strips sensitive params case-insensitively and keeps the rest", () => {
    expect(analyticsPath("/activate", "?CODE=ACDF-2345&x=1")).toBe("/activate?x=1")
  })

  it("keeps non-sensitive query parameters", () => {
    expect(analyticsPath("/compare", "?models=a,b")).toBe("/compare?models=a%2Cb")
  })

  it("returns the bare path when nothing removable is present", () => {
    expect(analyticsPath("/docs", "")).toBe("/docs")
  })
})
