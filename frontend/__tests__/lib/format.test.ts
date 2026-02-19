import { describe, it, expect } from "vitest"
import {
  formatPrice,
  formatContext,
  formatCurrency,
  formatDelta,
  formatAge,
  formatRelative,
} from "@/lib/format"

describe("formatPrice", () => {
  it("formats a typical price with 4 decimal places", () => {
    expect(formatPrice(2.5)).toBe("$2.5000")
  })

  it("formats zero", () => {
    expect(formatPrice(0)).toBe("$0.0000")
  })

  it("formats a very small price", () => {
    expect(formatPrice(0.0001)).toBe("$0.0001")
  })

  it("formats a large price", () => {
    expect(formatPrice(150)).toBe("$150.0000")
  })
})

describe("formatContext", () => {
  it("returns raw number for values < 1000", () => {
    expect(formatContext(512)).toBe("512")
  })

  it("formats thousands as K", () => {
    expect(formatContext(4096)).toBe("4K")
    expect(formatContext(128000)).toBe("128K")
  })

  it("formats millions as M", () => {
    expect(formatContext(1000000)).toBe("1M")
    expect(formatContext(2000000)).toBe("2M")
  })

  it("handles zero", () => {
    expect(formatContext(0)).toBe("0")
  })
})

describe("formatCurrency", () => {
  it("formats as USD with 2 decimal places", () => {
    expect(formatCurrency(10.5)).toBe("$10.50")
  })

  it("formats zero", () => {
    expect(formatCurrency(0)).toBe("$0.00")
  })

  it("formats large numbers with comma separators", () => {
    expect(formatCurrency(1234.56)).toBe("$1,234.56")
  })
})

describe("formatDelta", () => {
  it("formats positive delta with + sign", () => {
    expect(formatDelta(12.3)).toBe("+12.3%")
  })

  it("formats negative delta with - sign", () => {
    expect(formatDelta(-5.7)).toBe("-5.7%")
  })

  it("formats zero delta with + sign", () => {
    expect(formatDelta(0)).toBe("+0.0%")
  })
})

describe("formatAge", () => {
  it("returns '< 1h ago' for sub-hour values", () => {
    expect(formatAge(0.5)).toBe("< 1h ago")
  })

  it("formats hours", () => {
    expect(formatAge(3)).toBe("3h ago")
    expect(formatAge(23.7)).toBe("24h ago")
  })

  it("formats days for 24+ hours", () => {
    expect(formatAge(48)).toBe("2d ago")
    expect(formatAge(72)).toBe("3d ago")
  })
})

describe("formatRelative", () => {
  it("returns 'just now' for very recent timestamps", () => {
    const now = new Date().toISOString()
    expect(formatRelative(now)).toBe("just now")
  })

  it("formats minutes", () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60_000).toISOString()
    expect(formatRelative(fiveMinAgo)).toBe("5m ago")
  })

  it("formats hours", () => {
    const twoHoursAgo = new Date(Date.now() - 2 * 60 * 60_000).toISOString()
    expect(formatRelative(twoHoursAgo)).toBe("2h ago")
  })

  it("formats days", () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 24 * 60 * 60_000).toISOString()
    expect(formatRelative(threeDaysAgo)).toBe("3d ago")
  })
})
