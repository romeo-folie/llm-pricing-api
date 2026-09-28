import { describe, expect, it } from "vitest"
import type { Model } from "@/lib/api"
import {
  MIN_CONTEXT_OPTIONS,
  MODALITIES,
  filterModelsByQuery,
  paginate,
  parseModelsSearchParams,
  resolveProvider,
} from "@/lib/models-filter"

function model(name: string, slug: string): Model {
  return {
    id: slug,
    name,
    slug,
    provider: "p",
    modality: "text",
    context_window: 0,
    input_price_per_m: 0,
    output_price_per_m: 0,
    updated_at: "",
    underlying_provider: null,
    trust: {
      confirmed_at: "",
      source: "openrouter",
      confidence: "high",
      age_hours: 0,
      change_velocity: 0,
    },
  }
}

const catalog = [
  model("GPT-4o", "openai/gpt-4o"),
  model("Claude Sonnet 5", "anthropic/claude-sonnet-5"),
  model("Gemini 3 Pro", "google/gemini-3-pro"),
]

describe("filterModelsByQuery", () => {
  it("returns everything for an absent or blank query", () => {
    expect(filterModelsByQuery(catalog, undefined)).toEqual(catalog)
    expect(filterModelsByQuery(catalog, "")).toEqual(catalog)
    expect(filterModelsByQuery(catalog, "   ")).toEqual(catalog)
  })

  it("matches the name case-insensitively", () => {
    expect(filterModelsByQuery(catalog, "sonnet").map((m) => m.slug)).toEqual([
      "anthropic/claude-sonnet-5",
    ])
  })

  it("matches the slug", () => {
    expect(filterModelsByQuery(catalog, "OPENAI/").map((m) => m.slug)).toEqual(["openai/gpt-4o"])
  })

  it("trims surrounding whitespace", () => {
    expect(filterModelsByQuery(catalog, "  gemini ")).toHaveLength(1)
  })

  it("treats SQL wildcard characters literally", () => {
    expect(filterModelsByQuery(catalog, "%")).toEqual([])
    expect(filterModelsByQuery(catalog, "gpt_4o")).toEqual([])
  })

  it("preserves input order", () => {
    expect(filterModelsByQuery(catalog, "o").map((m) => m.slug)).toEqual(
      catalog.filter((m) => /o/i.test(m.name) || /o/i.test(m.slug)).map((m) => m.slug),
    )
  })
})

describe("paginate", () => {
  const items = Array.from({ length: 50 }, (_, i) => i)

  it("returns the requested slice and the full total", () => {
    expect(paginate(items, 2, 24)).toEqual({ data: items.slice(24, 48), total: 50 })
  })

  it("returns a short final page", () => {
    expect(paginate(items, 3, 24)).toEqual({ data: [48, 49], total: 50 })
  })

  it("returns an empty page past the end, still reporting the total", () => {
    expect(paginate(items, 9, 24)).toEqual({ data: [], total: 50 })
  })

  it("treats invalid page numbers as page 1", () => {
    expect(paginate(items, 0, 24).data).toEqual(items.slice(0, 24))
    expect(paginate(items, Number.NaN, 24).data).toEqual(items.slice(0, 24))
  })
})

describe("parseModelsSearchParams", () => {
  it("passes through valid values", () => {
    expect(
      parseModelsSearchParams({
        provider: "openai",
        modality: "text",
        min_context: "128000",
        q: "gpt",
        page: "3",
      }),
    ).toEqual({ provider: "openai", modality: "text", min_context: 128000, q: "gpt", page: 3 })
  })

  it("defaults everything when empty", () => {
    expect(parseModelsSearchParams({})).toEqual({
      provider: undefined,
      modality: undefined,
      min_context: undefined,
      q: undefined,
      page: 1,
    })
  })

  it("lowercases and trims the provider so casing variants share a cache key", () => {
    expect(parseModelsSearchParams({ provider: "  OpenAI " }).provider).toBe("openai")
  })

  it("drops overlong providers; existence is checked later by resolveProvider", () => {
    expect(parseModelsSearchParams({ provider: "x".repeat(65) }).provider).toBeUndefined()
    expect(parseModelsSearchParams({ provider: "fireworks_ai" }).provider).toBe("fireworks_ai")
  })

  it("drops modalities outside the known set", () => {
    expect(parseModelsSearchParams({ modality: "video" }).modality).toBeUndefined()
    for (const m of MODALITIES) {
      expect(parseModelsSearchParams({ modality: m }).modality).toBe(m)
    }
  })

  it("drops min_context values outside the dropdown options", () => {
    expect(parseModelsSearchParams({ min_context: "12345" }).min_context).toBeUndefined()
    expect(parseModelsSearchParams({ min_context: "abc" }).min_context).toBeUndefined()
    for (const v of MIN_CONTEXT_OPTIONS) {
      expect(parseModelsSearchParams({ min_context: String(v.value) }).min_context).toBe(v.value)
    }
  })

  it("clamps page to a positive integer", () => {
    expect(parseModelsSearchParams({ page: "-4" }).page).toBe(1)
    expect(parseModelsSearchParams({ page: "abc" }).page).toBe(1)
    expect(parseModelsSearchParams({ page: "2.7" }).page).toBe(2)
  })

  it("treats a blank q as absent", () => {
    expect(parseModelsSearchParams({ q: "  " }).q).toBeUndefined()
  })

  it("uses the first value when a param is repeated", () => {
    expect(parseModelsSearchParams({ provider: ["openai", "anthropic"] }).provider).toBe("openai")
  })
})

describe("resolveProvider", () => {
  const providers = [
    { id: "openai", name: "openai", model_count: 10 },
    { id: "Fireworks_AI", name: "Fireworks_AI", model_count: 3 },
  ]

  it("accepts a provider that exists, returning its canonical id", () => {
    expect(resolveProvider("openai", providers)).toBe("openai")
    expect(resolveProvider("fireworks_ai", providers)).toBe("Fireworks_AI")
  })

  it("rejects providers that do not exist so they cannot mint cache keys", () => {
    expect(resolveProvider("zz1", providers)).toBeUndefined()
    // `_` is an ILIKE wildcard upstream; it must not alias a real provider.
    expect(resolveProvider("o_enai", providers)).toBeUndefined()
    expect(resolveProvider("%", providers)).toBeUndefined()
  })

  it("returns undefined for an absent provider", () => {
    expect(resolveProvider(undefined, providers)).toBeUndefined()
  })
})
