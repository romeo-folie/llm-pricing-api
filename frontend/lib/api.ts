import "server-only"

// ─── Types ────────────────────────────────────────────────────────────────

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
  provider: string
  modality: string
  context_window: number
  input_price_per_m: number
  output_price_per_m: number
  updated_at: string
  trust: TrustMetadata
}

export interface PriceHistoryEntry {
  timestamp: string
  input_price_per_m: number
  output_price_per_m: number
  source: string
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

export interface ApiResponse<T> {
  data: T
  meta?: {
    total?: number
    page?: number
    per_page?: number
  }
}

// ─── Config ───────────────────────────────────────────────────────────────

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
    const body = await res.json().catch(() => ({})) as { detail?: string }
    throw new Error(
      `API error ${res.status} at ${path}${body.detail ? `: ${body.detail}` : ""}`,
    )
  }
  return res.json() as Promise<T>
}

// ─── Endpoints ────────────────────────────────────────────────────────────

export interface ModelsFilter {
  provider?: string
  modality?: string
  min_context?: number
}

export async function getModels(filter?: ModelsFilter): Promise<Model[]> {
  const params = new URLSearchParams()
  if (filter?.provider)    params.set("provider", filter.provider)
  if (filter?.modality)    params.set("modality", filter.modality)
  if (filter?.min_context) params.set("min_context", String(filter.min_context))
  const qs = params.toString() ? `?${params.toString()}` : ""
  const res = await apiFetch<ApiResponse<Model[]>>(`/v1/models${qs}`)
  return res.data
}

export async function getModel(id: string): Promise<Model> {
  const res = await apiFetch<ApiResponse<Model>>(`/v1/models/${encodeURIComponent(id)}`)
  return res.data
}

export async function getModelHistory(
  id: string,
  from?: string,
  to?: string,
): Promise<PriceHistoryEntry[]> {
  const params = new URLSearchParams()
  if (from) params.set("from", from)
  if (to)   params.set("to", to)
  const qs = params.toString() ? `?${params.toString()}` : ""
  const res = await apiFetch<ApiResponse<PriceHistoryEntry[]>>(
    `/v1/models/${encodeURIComponent(id)}/history${qs}`,
    { next: { revalidate: 60 } },
  )
  return res.data
}

export async function getProviders(): Promise<Provider[]> {
  const res = await apiFetch<ApiResponse<Provider[]>>("/v1/providers")
  return res.data
}

export interface CompareFilter {
  models: string[]
}

export async function getCompare(models: string[]): Promise<Model[]> {
  const qs = models.map((m) => `models=${encodeURIComponent(m)}`).join("&")
  const res = await apiFetch<ApiResponse<Model[]>>(`/v1/compare?${qs}`)
  return res.data
}

export interface ChangesFilter {
  since?: string
  provider?: string
}

export async function getChanges(filter?: ChangesFilter): Promise<PriceChange[]> {
  const params = new URLSearchParams()
  if (filter?.since)    params.set("since", filter.since)
  if (filter?.provider) params.set("provider", filter.provider)
  const qs = params.toString() ? `?${params.toString()}` : ""
  // /v1/changes uses the default revalidate: 300. Live updates are handled
  // by the client-side poller in the /changes page (hitting /api/changes route).
  const res = await apiFetch<ApiResponse<PriceChange[]>>(`/v1/changes${qs}`)
  return res.data
}
