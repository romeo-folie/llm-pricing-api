import { describe, expect, it } from "vitest"
import { metadata } from "@/app/signup/free/page"

describe("signup metadata", () => {
  it("keeps the transactional signup page out of search results", () => {
    expect(metadata.robots).toEqual({ index: false, follow: true })
    expect(metadata.alternates).toEqual({ canonical: "/signup/free" })
  })
})
