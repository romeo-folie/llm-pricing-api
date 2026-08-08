import { describe, it, expect, vi, beforeEach } from "vitest"
import {
  getModels,
  getModel,
  getModelHistory,
  getProviders,
  getCompare,
  getChanges,
} from "@/lib/api"

// ─── Mock fetch ──────────────────────────────────────────────────────────────
// We mock global fetch to return raw backend shapes, then verify the exported
// functions transform them into the correct frontend types.

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

beforeEach(() => {
  mockFetch.mockReset()
})

// ─── Fixture factories ──────────────────────────────────────────────────────

function rawModel(overrides = {}) {
  return {
    id: 42,
    provider: "openai",
    name: "GPT-4o",
    slug: "gpt-4o",
    modality: "text",
    context_window: 128000,
    price_input: 0.0000025,   // $2.50 per million
    price_output: 0.000010,   // $10.00 per million
    underlying_provider: null as string | null,
    meta: {
      confirmed_at: "2026-02-18T12:00:00Z",
      source: "openrouter",
      confidence: "high" as const,
      age_hours: 2.5,
      change_velocity: 0.1,
    },
    ...overrides,
  }
}

function rawChange(overrides = {}) {
  return {
    model_id: 42,
    model_slug: "gpt-4o",
    provider: "openai",
    old_input: 0.000003,
    old_output: 0.000012,
    new_input: 0.0000025,
    new_output: 0.000010,
    confirmed_at: "2026-02-18T12:00:00Z",
    source: "openrouter",
    ...overrides,
  }
}

function rawHistoryItem(overrides = {}) {
  return {
    input_cost_per_token: 0.0000025,
    output_cost_per_token: 0.000010,
    source: "openrouter",
    underlying_provider: null as string | null,
    confirmed_at: "2026-02-18T12:00:00Z",
    recorded_at: "2026-02-18T12:01:00Z",
    ...overrides,
  }
}

function rawProvider(overrides = {}) {
  return {
    name: "openai",
    model_count: 15,
    ...overrides,
  }
}

function envelope<T>(data: T) {
  return {
    data,
    meta: {
      confirmed_at: "2026-02-18T12:00:00Z",
      source: "openrouter",
      confidence: "high" as const,
      age_hours: 2.5,
      change_velocity: 0.1,
    },
  }
}

function mockOk(body: unknown, headers: Record<string, string> = {}) {
  // First call returns the provided body
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(body),
    headers: new Headers(headers),
  })
  // Subsequent calls return an empty envelope to break pagination loops
  mockFetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(envelope([])),
    headers: new Headers({ "X-Total-Count": "0" }),
  })
}

// ─── Tests ──────────────────────────────────────────────────────────────────

describe("getModels", () => {
  it("transforms integer IDs to strings", async () => {
    mockOk(envelope([rawModel({ id: 42 })]))
    const models = await getModels()
    expect(models[0].id).toBe("42")
  })

  it("converts per-token prices to per-million", async () => {
    mockOk(envelope([rawModel({ price_input: 0.0000025, price_output: 0.000010 })]))
    const models = await getModels()
    expect(models[0].input_price_per_m).toBeCloseTo(2.5, 4)
    expect(models[0].output_price_per_m).toBeCloseTo(10, 4)
  })

  it("maps backend field names to frontend field names", async () => {
    mockOk(envelope([rawModel()]))
    const models = await getModels()
    const m = models[0]
    expect(m).toHaveProperty("name")
    expect(m).toHaveProperty("slug")
    expect(m).toHaveProperty("provider")
    expect(m).toHaveProperty("modality")
    expect(m).toHaveProperty("context_window")
    expect(m).toHaveProperty("input_price_per_m")
    expect(m).toHaveProperty("output_price_per_m")
    expect(m).toHaveProperty("trust")
    expect(m.trust).toHaveProperty("confidence")
    expect(m.trust).toHaveProperty("age_hours")
  })

  it("defaults null context_window to 0", async () => {
    mockOk(envelope([rawModel({ context_window: null })]))
    const models = await getModels()
    expect(models[0].context_window).toBe(0)
  })

  it("maps underlying_provider when present", async () => {
    mockOk(envelope([rawModel({ underlying_provider: "together" })]))
    const models = await getModels()
    expect(models[0].underlying_provider).toBe("together")
  })

  it("defaults underlying_provider to null when absent", async () => {
    const raw = rawModel()
    delete (raw as Record<string, unknown>).underlying_provider
    mockOk(envelope([raw]))
    const models = await getModels()
    expect(models[0].underlying_provider).toBeNull()
  })

  it("handles huggingface_inference_providers source", async () => {
    mockOk(envelope([rawModel({
      meta: {
        confirmed_at: "2026-02-18T12:00:00Z",
        source: "huggingface_inference_providers",
        confidence: "high" as const,
        age_hours: 1,
        change_velocity: 0,
      },
      underlying_provider: "together",
    })]))
    const models = await getModels()
    expect(models[0].trust.source).toBe("huggingface_inference_providers")
    expect(models[0].underlying_provider).toBe("together")
  })

  it("applies query filters", async () => {
    mockOk(envelope([]))
    await getModels({ provider: "anthropic", modality: "text", min_context: 128000 })
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("provider=anthropic")
    expect(url).toContain("modality=text")
    expect(url).toContain("min_context=128000")
  })
})

describe("getModel", () => {
  it("returns a single transformed model", async () => {
    mockOk(envelope(rawModel({ id: 7, name: "Claude Sonnet" })))
    const model = await getModel("7")
    expect(model.id).toBe("7")
    expect(model.name).toBe("Claude Sonnet")
  })

  it("encodes the ID in the URL", async () => {
    mockOk(envelope(rawModel()))
    await getModel("some/id")
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("some%2Fid")
  })
})

describe("getModelHistory", () => {
  it("converts per-token history prices to per-million", async () => {
    mockOk(envelope([rawHistoryItem({ input_cost_per_token: 0.000003, output_cost_per_token: 0.000012 })]))
    const history = await getModelHistory("42")
    expect(history[0].input_price_per_m).toBeCloseTo(3, 4)
    expect(history[0].output_price_per_m).toBeCloseTo(12, 4)
  })

  it("maps confirmed_at to timestamp", async () => {
    mockOk(envelope([rawHistoryItem({ confirmed_at: "2026-01-15T10:00:00Z" })]))
    const history = await getModelHistory("42")
    expect(history[0].timestamp).toBe("2026-01-15T10:00:00Z")
  })

  it("passes from/to query params", async () => {
    mockOk(envelope([]))
    await getModelHistory("42", "2026-01-01", "2026-02-01")
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("from=2026-01-01")
    expect(url).toContain("to=2026-02-01")
  })

  it("maps underlying_provider through to history entry", async () => {
    mockOk(envelope([rawHistoryItem({ underlying_provider: "replicate" })]))
    const history = await getModelHistory("42")
    expect(history[0].underlying_provider).toBe("replicate")
  })

  it("defaults underlying_provider to null for history entries", async () => {
    const raw = rawHistoryItem()
    delete (raw as Record<string, unknown>).underlying_provider
    mockOk(envelope([raw]))
    const history = await getModelHistory("42")
    expect(history[0].underlying_provider).toBeNull()
  })
})

describe("getProviders", () => {
  it("transforms provider names to id/name", async () => {
    mockOk(envelope([rawProvider({ name: "anthropic", model_count: 8 })]))
    const providers = await getProviders()
    expect(providers[0]).toEqual({
      id: "anthropic",
      name: "anthropic",
      model_count: 8,
    })
  })
})

describe("getCompare", () => {
  it("sends comma-separated, encoded model slugs", async () => {
    mockOk(envelope({ items: [] }))
    await getCompare(["nvidia/nemotron", "deepseek/v4-flash"])
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("models=nvidia%2Fnemotron,deepseek%2Fv4-flash")
  })

  it("transforms models from the nested compare response", async () => {
    mockOk(envelope({
      items: [rawModel({ id: 1 }), rawModel({ id: 2 })],
      warnings: ["Limited benchmark coverage"],
    }))
    const models = await getCompare(["nvidia/nemotron", "deepseek/v4-flash"])
    expect(models).toHaveLength(2)
    expect(models[0].id).toBe("1")
    expect(models[1].id).toBe("2")
  })
})

describe("getChanges", () => {
  it("converts per-token change prices to per-million", async () => {
    mockOk(envelope([rawChange({
      old_input: 0.000003,
      new_input: 0.0000025,
      old_output: 0.000012,
      new_output: 0.000010,
    })]))
    const changes = await getChanges()
    const c = changes[0]
    expect(c.old_input_price).toBeCloseTo(3, 4)
    expect(c.new_input_price).toBeCloseTo(2.5, 4)
    expect(c.old_output_price).toBeCloseTo(12, 4)
    expect(c.new_output_price).toBeCloseTo(10, 4)
  })

  it("computes delta_pct from old and new totals", async () => {
    // Old total: 3 + 12 = 15 (per M)
    // New total: 2.5 + 10 = 12.5 (per M)
    // Delta: (12.5 - 15) / 15 * 100 = -16.667%
    mockOk(envelope([rawChange({
      old_input: 0.000003,
      new_input: 0.0000025,
      old_output: 0.000012,
      new_output: 0.000010,
    })]))
    const changes = await getChanges()
    expect(changes[0].delta_pct).toBeCloseTo(-16.667, 1)
  })

  it("generates deterministic ID from model_id + timestamp", async () => {
    mockOk(envelope([rawChange({ model_id: 42, confirmed_at: "2026-02-18T12:00:00Z" })]))
    const changes = await getChanges()
    expect(changes[0].id).toBe("42-2026-02-18T12:00:00Z")
  })

  it("maps model_slug to model_name", async () => {
    mockOk(envelope([rawChange({ model_slug: "gpt-4o" })]))
    const changes = await getChanges()
    expect(changes[0].model_name).toBe("gpt-4o")
  })

  it("converts bare date to RFC3339 for since filter", async () => {
    mockOk(envelope([]))
    await getChanges({ since: "2026-01-15" })
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("since=2026-01-15T00%3A00%3A00Z")
  })

  it("passes through RFC3339 since filter as-is", async () => {
    mockOk(envelope([]))
    await getChanges({ since: "2026-01-15T10:30:00Z" })
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain("since=2026-01-15T10")
  })

  it("handles zero old total without division by zero", async () => {
    mockOk(envelope([rawChange({ old_input: 0, old_output: 0 })]))
    const changes = await getChanges()
    expect(changes[0].delta_pct).toBe(0)
  })
})

describe("error handling", () => {
  it("throws with status and detail from RFC 7807 error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 404,
      json: () => Promise.resolve({ detail: "Model not found" }),
    })
    await expect(getModel("999")).rejects.toThrow("API error 404")
  })

  it("throws with status even if body is not JSON", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      json: () => Promise.reject(new Error("not json")),
    })
    await expect(getModel("999")).rejects.toThrow("API error 500")
  })
})
