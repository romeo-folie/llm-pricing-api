import "server-only"
import { cache } from "react"

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
    input_price_per_m:  (raw.price_input  ?? 0) * PER_MILLION,
    output_price_per_m: (raw.price_output ?? 0) * PER_MILLION,
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

  // Only include fields that previously had a non-zero price in the delta.
  // A field going $0→$X is new pricing data being populated, not a price
  // change — including it inflates delta_pct dramatically.
  const oldSum = (oldInputM > 0 ? oldInputM : 0) + (oldOutputM > 0 ? oldOutputM : 0)
  const newSum = (oldInputM > 0 ? newInputM : 0) + (oldOutputM > 0 ? newOutputM : 0)
  const deltaPct = oldSum > 0 ? ((newSum - oldSum) / oldSum) * 100 : 0

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

const BASE_URL =
  process.env.LLM_PRICING_API_BASE_URL ||
  (process.env.NODE_ENV === "production"
    ? "https://api.llmrates.live"
    : "http://localhost:8080")

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

/** Variant of apiFetch that also returns response headers (e.g. for X-Total-Count). */
async function apiFetchWithHeaders<T>(
  path: string,
  init?: RequestInit,
): Promise<{ body: T; headers: Headers }> {
  const res = await fetch(`${BASE_URL}${path}`, {
    next: { revalidate: 300 },
    ...init,
    headers: buildHeaders(init?.headers as HeadersInit | undefined),
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string }
    throw new Error(
      `API error ${res.status} at ${path}${body.detail ? `: ${body.detail}` : ""}`,
    )
  }
  const body = (await res.json()) as T
  return { body, headers: res.headers }
}

// ─── Paginated Result ───────────────────────────────────────────────────────

export interface PaginatedResult<T> {
  data: T
  total: number
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

export interface ModelsFilter {
  provider?: string
  modality?: string
  min_context?: number
  q?: string
}

export async function getModels(filter?: ModelsFilter): Promise<Model[]> {
  const params = new URLSearchParams()
  if (filter?.provider) params.set("provider", filter.provider)
  if (filter?.modality) params.set("modality", filter.modality)
  if (filter?.min_context) params.set("min_context", String(filter.min_context))
  if (filter?.q) params.set("q", filter.q)
  params.set("per_page", "200")

  // Paginate through all pages so that models beyond the first 200 are
  // included in the compare picker. The backend caps per_page at 200, so we
  // loop until X-Total-Count is satisfied.
  const all: Model[] = []
  let page = 1
  let total = Infinity

  while (all.length < total) {
    params.set("page", String(page))
    const { body, headers } = await apiFetchWithHeaders<RawEnvelope<RawModel[]>>(
      `/v1/models?${params.toString()}`,
    )
    const batch = body.data.map(toModel)

    if (page === 1) {
      // Only narrow `total` when the header is present and parseable.
      // If the header is absent or invalid we keep total = Infinity and rely
      // on the batch.length === 0 exit condition + the page-50 hard cap.
      const raw    = headers.get("X-Total-Count")
      const parsed = raw !== null ? parseInt(raw, 10) : NaN
      if (!isNaN(parsed) && parsed > 0) total = parsed
    }

    all.push(...batch)
    // Hard cap: never exceed 50 pages (10 000 models) regardless of header.
    if (batch.length === 0 || all.length >= total || page >= 50) break
    page++
  }

  return all
}

/** Paginated variant — returns a page of models + total count from X-Total-Count header. */
export async function getModelsPaginated(
  filter?: ModelsFilter & { page?: number; per_page?: number },
): Promise<PaginatedResult<Model[]>> {
  const params = new URLSearchParams()
  if (filter?.provider) params.set("provider", filter.provider)
  if (filter?.modality) params.set("modality", filter.modality)
  if (filter?.min_context) params.set("min_context", String(filter.min_context))
  if (filter?.q) params.set("q", filter.q)
  params.set("page", String(filter?.page ?? 1))
  params.set("per_page", String(filter?.per_page ?? 24))
  const qs = `?${params.toString()}`
  const { body, headers } = await apiFetchWithHeaders<RawEnvelope<RawModel[]>>(
    `/v1/models${qs}`,
  )
  const total = parseInt(headers.get("X-Total-Count") ?? "0", 10)
  return {
    data: body.data.map(toModel),
    total,
  }
}

// React.cache() deduplicates calls with the same argument within the same
// server request (shared cache scope covers generateMetadata + page component),
// preventing two upstream API calls per page render.
export const getModel = cache(async (id: string): Promise<Model> => {
  const res = await apiFetch<RawEnvelope<RawModel>>(
    `/v1/models/${encodeURIComponent(id)}`,
  )
  return toModel(res.data)
})

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

export const getProviders = cache(async (): Promise<Provider[]> => {
  const res = await apiFetch<RawEnvelope<RawProvider[]>>("/v1/providers")
  return res.data.map(toProvider)
})

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

// ─── Paginated Changes ──────────────────────────────────────────────────────

export interface ChangesPageFilter {
  since?: string
  before?: string
  provider?: string
  limit?: number
}

export interface ChangesPageResult {
  data: PriceChange[]
  total: number
  hasMore: boolean
  nextCursor: string | null
}

/** Fetch a single page of changes using cursor-based pagination. */
export async function getChangesPage(
  filter?: ChangesPageFilter,
): Promise<ChangesPageResult> {
  const params = new URLSearchParams()
  if (filter?.since) {
    const since = filter.since.includes("T")
      ? filter.since
      : `${filter.since}T00:00:00Z`
    params.set("since", since)
  }
  if (filter?.before) params.set("before", filter.before)
  if (filter?.provider) params.set("provider", filter.provider)
  if (filter?.limit) params.set("limit", String(filter.limit))
  const qs = params.toString() ? `?${params.toString()}` : ""
  const { body, headers } = await apiFetchWithHeaders<RawEnvelope<RawChange[]>>(
    `/v1/changes${qs}`,
  )
  const total = parseInt(headers.get("X-Total-Count") ?? "0", 10)
  const hasMore = headers.get("X-Has-More") === "true"
  const nextCursor = headers.get("X-Next-Cursor") || null
  return {
    data: body.data.map(toChange),
    total,
    hasMore,
    nextCursor,
  }
}

// ─── Changes Summary ────────────────────────────────────────────────────────

interface RawHeatmapModelNode {
  model_id: number
  model_slug: string
  change_count: number
  avg_delta_pct: number
}

interface RawHeatmapProviderGroup {
  provider: string
  total_changes: number
  models: RawHeatmapModelNode[]
}

interface RawTopMover {
  model_id: number
  model_slug: string
  provider: string
  delta_pct: number
  old_input: number
  old_output: number
  new_input: number
  new_output: number
  source: string
  confirmed_at: string
}

interface RawChangesSummary {
  heatmap: RawHeatmapProviderGroup[]
  top_movers: RawTopMover[]
  total_changes: number
  window: string
}

export interface HeatmapModelNode {
  model_id: string
  model_slug: string
  change_count: number
  avg_delta_pct: number
}

export interface HeatmapProviderGroup {
  provider: string
  total_changes: number
  models: HeatmapModelNode[]
}

export interface TopMover {
  model_id: string
  model_slug: string
  provider: string
  delta_pct: number
  old_input_price: number
  old_output_price: number
  new_input_price: number
  new_output_price: number
  source: string
  confirmed_at: string
}

export interface ChangesSummary {
  heatmap: HeatmapProviderGroup[]
  top_movers: TopMover[]
  total_changes: number
  window: string
}

function toSummary(raw: RawChangesSummary): ChangesSummary {
  return {
    heatmap: (raw.heatmap ?? []).map((g) => ({
      provider: g.provider,
      total_changes: g.total_changes,
      models: (g.models ?? []).map((m) => ({
        model_id: String(m.model_id),
        model_slug: m.model_slug,
        change_count: m.change_count,
        avg_delta_pct: m.avg_delta_pct,
      })),
    })),
    top_movers: (raw.top_movers ?? []).map((m) => ({
      model_id: String(m.model_id),
      model_slug: m.model_slug,
      provider: m.provider,
      delta_pct: m.delta_pct,
      old_input_price: m.old_input * PER_MILLION,
      old_output_price: m.old_output * PER_MILLION,
      new_input_price: m.new_input * PER_MILLION,
      new_output_price: m.new_output * PER_MILLION,
      source: m.source,
      confirmed_at: m.confirmed_at,
    })),
    total_changes: raw.total_changes,
    window: raw.window,
  }
}

export interface SummaryFilter {
  window?: string
  since?: string
  provider?: string
}

/** Fetch pre-aggregated summary data for heatmap and leaderboard. */
export async function getChangesSummary(
  filter?: SummaryFilter,
): Promise<ChangesSummary> {
  const params = new URLSearchParams()
  if (filter?.window) params.set("window", filter.window)
  if (filter?.since) params.set("since", filter.since)
  if (filter?.provider) params.set("provider", filter.provider)
  const qs = params.toString() ? `?${params.toString()}` : ""
  const res = await apiFetch<RawEnvelope<RawChangesSummary>>(
    `/v1/changes/summary${qs}`,
  )
  return toSummary(res.data)
}
