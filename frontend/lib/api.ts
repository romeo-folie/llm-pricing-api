import "server-only"

// ─── Types (frontend-facing) ─────────────────────────────────────────────────
// These types are what components consume. The API layer transforms raw backend
// responses into these shapes so that components never deal with backend quirks
// (integer IDs, per-token pricing, different field names, etc.).

export interface TrustMetadata {
  confirmed_at: string
  source: string
  confidence: "high" | "medium" | "low"
  age_hours: number
  change_velocity: number
}

export interface Model {
  id: string
  name: string
  slug: string
  provider: string
  modality: string
  context_window: number
  input_price_per_m: number
  output_price_per_m: number
  updated_at: string
  underlying_provider: string | null
  trust: TrustMetadata
}

export interface PriceHistoryEntry {
  timestamp: string
  input_price_per_m: number
  output_price_per_m: number
  source: string
  underlying_provider: string | null
}

export interface Provider {
  id: string
  name: string
  model_count: number
}

export interface PriceChange {
  id: string
  model_id: string
  model_name: string
  provider: string
  old_input_price: number
  new_input_price: number
  old_output_price: number
  new_output_price: number
  delta_pct: number
  source: string
  changed_at: string
}

// ─── Raw backend response types ──────────────────────────────────────────────
// These match the exact JSON shapes returned by the Go API.

interface RawTrustMeta {
  confirmed_at: string
  source: string
  confidence: "high" | "medium" | "low"
  age_hours: number
  change_velocity: number
}

interface RawModel {
  id: number
  provider: string
  name: string
  slug: string
  modality: string
  context_window: number | null
  price_input: number
  price_output: number
  underlying_provider?: string | null
  meta: RawTrustMeta
}

interface RawChange {
  model_id: number
  model_slug: string
  provider: string
  old_input: number
  old_output: number
  new_input: number
  new_output: number
  confirmed_at: string
  source: string
}

interface RawHistoryItem {
  input_cost_per_token: number
  output_cost_per_token: number
  source: string
  underlying_provider?: string | null
  confirmed_at: string
  recorded_at: string
}

interface RawProvider {
  name: string
  model_count: number
}

interface RawEnvelope<T> {
  data: T
  meta: RawTrustMeta
}

// ─── Transformers ────────────────────────────────────────────────────────────

const PER_MILLION = 1_000_000

function toModel(raw: RawModel): Model {
  return {
    id: String(raw.id),
    name: raw.name,
    slug: raw.slug,
    provider: raw.provider,
    modality: raw.modality,
    context_window: raw.context_window ?? 0,
    input_price_per_m: raw.price_input * PER_MILLION,
    output_price_per_m: raw.price_output * PER_MILLION,
    updated_at: raw.meta.confirmed_at ?? "",
    underlying_provider: raw.underlying_provider ?? null,
    trust: raw.meta,
  }
}

function toProvider(raw: RawProvider): Provider {
  return {
    id: raw.name,
    name: raw.name,
    model_count: raw.model_count,
  }
}

function toChange(raw: RawChange): PriceChange {
  const oldInputM = raw.old_input * PER_MILLION
  const newInputM = raw.new_input * PER_MILLION
  const oldOutputM = raw.old_output * PER_MILLION
  const newOutputM = raw.new_output * PER_MILLION

  const oldTotal = oldInputM + oldOutputM
  const deltaPct =
    oldTotal > 0
      ? ((newInputM + newOutputM - oldTotal) / oldTotal) * 100
      : 0

  return {
    // Deterministic ID from model + timestamp — stable across polling requests
    id: `${raw.model_id}-${raw.confirmed_at}`,
    model_id: String(raw.model_id),
    model_name: raw.model_slug,
    provider: raw.provider,
    old_input_price: oldInputM,
    new_input_price: newInputM,
    old_output_price: oldOutputM,
    new_output_price: newOutputM,
    delta_pct: deltaPct,
    source: raw.source,
    changed_at: raw.confirmed_at,
  }
}

function toHistoryEntry(raw: RawHistoryItem): PriceHistoryEntry {
  return {
    timestamp: raw.confirmed_at,
    input_price_per_m: raw.input_cost_per_token * PER_MILLION,
    output_price_per_m: raw.output_cost_per_token * PER_MILLION,
    source: raw.source,
    underlying_provider: raw.underlying_provider ?? null,
  }
}

// ─── Config ──────────────────────────────────────────────────────────────────

const BASE_URL = process.env.LLM_PRICING_API_BASE_URL || "http://localhost:8080"

// Headers constructed lazily per-request so that a missing API_KEY at module
// evaluation does not silently produce an empty bearer token that persists
// for the lifetime of the module.
function buildHeaders(extra?: HeadersInit): Record<string, string> {
  const apiKey = process.env.LLM_PRICING_API_KEY || ""
  return {
    "Content-Type": "application/json",
    "Authorization": `Bearer ${apiKey}`,
    ...(extra as Record<string, string> | undefined),
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    next: { revalidate: 300 },
    ...init,
    // Merge caller headers on top of auth headers so Authorization is never dropped
    headers: buildHeaders(init?.headers as HeadersInit | undefined),
  })
  if (!res.ok) {
    // Attempt to parse RFC 7807 structured error for actionable log output
    const body = (await res.json().catch(() => ({}))) as { detail?: string }
    throw new Error(
      `API error ${res.status} at ${path}${body.detail ? `: ${body.detail}` : ""}`,
    )
  }
  return res.json() as Promise<T>
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

export interface ModelsFilter {
  provider?: string
  modality?: string
  min_context?: number
}

export async function getModels(filter?: ModelsFilter): Promise<Model[]> {
  const params = new URLSearchParams()
  if (filter?.provider) params.set("provider", filter.provider)
  if (filter?.modality) params.set("modality", filter.modality)
  if (filter?.min_context) params.set("min_context", String(filter.min_context))
  // Request max page size to get all models in one call
  params.set("per_page", "200")
  const qs = `?${params.toString()}`
  const res = await apiFetch<RawEnvelope<RawModel[]>>(`/v1/models${qs}`)
  return res.data.map(toModel)
}

export async function getModel(id: string): Promise<Model> {
  const res = await apiFetch<RawEnvelope<RawModel>>(
    `/v1/models/${encodeURIComponent(id)}`,
  )
  return toModel(res.data)
}

export async function getModelHistory(
  id: string,
  from?: string,
  to?: string,
): Promise<PriceHistoryEntry[]> {
  const params = new URLSearchParams()
  if (from) params.set("from", from)
  if (to) params.set("to", to)
  const qs = params.toString() ? `?${params.toString()}` : ""
  const res = await apiFetch<RawEnvelope<RawHistoryItem[]>>(
    `/v1/models/${encodeURIComponent(id)}/history${qs}`,
    { next: { revalidate: 60 } },
  )
  return res.data.map(toHistoryEntry)
}

export async function getProviders(): Promise<Provider[]> {
  const res = await apiFetch<RawEnvelope<RawProvider[]>>("/v1/providers")
  return res.data.map(toProvider)
}

export interface CompareFilter {
  models: string[]
}

export async function getCompare(models: string[]): Promise<Model[]> {
  // Backend expects comma-separated integer IDs: ?models=1,2,3
  const qs = `models=${models.map(encodeURIComponent).join(",")}`
  const res = await apiFetch<RawEnvelope<RawModel[]>>(`/v1/compare?${qs}`)
  return res.data.map(toModel)
}

export interface ChangesFilter {
  since?: string
  provider?: string
}

export async function getChanges(
  filter?: ChangesFilter,
): Promise<PriceChange[]> {
  const params = new URLSearchParams()
  if (filter?.since) {
    // The date input gives "YYYY-MM-DD" but backend expects RFC3339.
    // Convert bare dates to full timestamps.
    const since = filter.since.includes("T")
      ? filter.since
      : `${filter.since}T00:00:00Z`
    params.set("since", since)
  }
  if (filter?.provider) params.set("provider", filter.provider)
  const qs = params.toString() ? `?${params.toString()}` : ""
  const res = await apiFetch<RawEnvelope<RawChange[]>>(`/v1/changes${qs}`)
  return res.data.map(toChange)
}
