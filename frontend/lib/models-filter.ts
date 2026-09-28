import type { Model, PaginatedResult, Provider } from "@/lib/api"

export const MODALITIES = ["text", "multimodal", "image", "audio", "embedding"] as const

export const MIN_CONTEXT_OPTIONS = [
  { value: 4096, label: "4K+" },
  { value: 32768, label: "32K+" },
  { value: 128000, label: "128K+" },
  { value: 1000000, label: "1M+" },
] as const

const MAX_PROVIDER_LENGTH = 64

type RawParam = string | string[] | undefined

export interface ModelsSearchParams {
  provider?: RawParam
  modality?: RawParam
  min_context?: RawParam
  q?: RawParam
  page?: RawParam
}

export interface ParsedModelsSearchParams {
  provider: string | undefined
  modality: string | undefined
  min_context: number | undefined
  q: string | undefined
  page: number
}

function first(v: RawParam): string | undefined {
  return Array.isArray(v) ? v[0] : v
}

// Each accepted value becomes part of a data-cache key, so only values the UI
// can produce are accepted; anything else is ignored rather than cached.
export function parseModelsSearchParams(sp: ModelsSearchParams): ParsedModelsSearchParams {
  const provider = first(sp.provider)?.trim().toLowerCase()
  const modality = first(sp.modality)
  const minContext = Number(first(sp.min_context))
  const q = first(sp.q)?.trim()
  const page = Math.floor(Number(first(sp.page)))

  return {
    provider: provider && provider.length <= MAX_PROVIDER_LENGTH ? provider : undefined,
    modality: (MODALITIES as readonly string[]).includes(modality ?? "") ? modality : undefined,
    min_context: MIN_CONTEXT_OPTIONS.some((o) => o.value === minContext) ? minContext : undefined,
    q: q || undefined,
    page: Number.isFinite(page) && page >= 1 ? page : 1,
  }
}

// Any unknown provider would be its own cache entry, and `%`/`_` are ILIKE
// wildcards upstream, so only providers the API actually lists are accepted.
export function resolveProvider(
  provider: string | undefined,
  providers: Provider[],
): string | undefined {
  const wanted = provider?.trim().toLowerCase()
  if (!wanted) return undefined
  return providers.find((p) => p.id.toLowerCase() === wanted)?.id
}

// Mirrors the backend's name/slug ILIKE search, but matches the query as a
// literal substring rather than letting % and _ act as wildcards.
export function filterModelsByQuery(models: Model[], q: string | undefined): Model[] {
  const needle = q?.trim().toLowerCase()
  if (!needle) return models
  return models.filter(
    (m) => m.name.toLowerCase().includes(needle) || m.slug.toLowerCase().includes(needle),
  )
}

export function paginate<T>(items: T[], page: number, perPage: number): PaginatedResult<T[]> {
  const p = Number.isFinite(page) && page >= 1 ? Math.floor(page) : 1
  const start = (p - 1) * perPage
  return { data: items.slice(start, start + perPage), total: items.length }
}
