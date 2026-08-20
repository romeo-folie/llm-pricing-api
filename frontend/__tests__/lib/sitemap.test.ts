import { describe, expect, it } from "vitest"
import type { Model } from "@/lib/api"
import {
  MAX_SITEMAP_MODELS,
  SITEMAP_MODEL_MAX_AGE_DAYS,
  encodeSlugPath,
  isCanonicalPublicSlug,
  selectSitemapModels,
} from "@/lib/sitemap"

const NOW = new Date("2026-08-20T12:00:00Z")

function model(overrides: Partial<Model> = {}): Model {
  return {
    id: "1",
    name: "Current Model",
    slug: "provider/current-model",
    provider: "provider",
    modality: "text",
    context_window: 128_000,
    input_price_per_m: 1,
    output_price_per_m: 2,
    updated_at: "2026-08-20T08:00:00Z",
    underlying_provider: null,
    trust: {
      confirmed_at: "2026-08-20T08:00:00Z",
      source: "provider_docs",
      confidence: "high",
      age_hours: 4,
      change_velocity: 0,
    },
    ...overrides,
  }
}

describe("isCanonicalPublicSlug", () => {
  it("accepts normal nested model slugs", () => {
    expect(isCanonicalPublicSlug("openai/gpt-5.6-sol")).toBe(true)
  })

  it.each([
    "openai/gpt-4o:batch",
    "qwen/qwen-plus:thinking",
    "~openai/gpt-latest",
    "openai/model name",
    "openai//model",
  ])("rejects non-canonical or unsafe slug %s", (slug) => {
    expect(isCanonicalPublicSlug(slug)).toBe(false)
  })
})

describe("encodeSlugPath", () => {
  it("preserves route separators while encoding each segment", () => {
    expect(encodeSlugPath("azure_ai/FW-GLM-5")).toBe("azure_ai/FW-GLM-5")
  })
})

describe("selectSitemapModels", () => {
  it(`keeps only priced models confirmed in the latest ${SITEMAP_MODEL_MAX_AGE_DAYS} days`, () => {
    const selected = selectSitemapModels([
      model(),
      model({ id: "2", slug: "provider/stale", updated_at: "2026-04-01T00:00:00Z" }),
      model({ id: "3", slug: "provider/unpriced", input_price_per_m: 0, output_price_per_m: 0 }),
      model({ id: "4", slug: "provider/batch:batch" }),
    ], NOW)

    expect(selected.map((item) => item.slug)).toEqual(["provider/current-model"])
  })

  it("deduplicates slugs and orders the newest confirmation first", () => {
    const selected = selectSitemapModels([
      model({ id: "1", slug: "provider/older", updated_at: "2026-08-18T00:00:00Z" }),
      model({ id: "2", slug: "provider/newer", updated_at: "2026-08-20T00:00:00Z" }),
      model({ id: "3", slug: "provider/newer", updated_at: "2026-08-19T00:00:00Z" }),
    ], NOW)

    expect(selected.map((item) => item.slug)).toEqual([
      "provider/newer",
      "provider/older",
    ])
  })

  it("caps the discovery set", () => {
    const models = Array.from({ length: MAX_SITEMAP_MODELS + 10 }, (_, index) =>
      model({ id: String(index), slug: `provider/model-${index}` }),
    )

    expect(selectSitemapModels(models, NOW)).toHaveLength(MAX_SITEMAP_MODELS)
  })
})
