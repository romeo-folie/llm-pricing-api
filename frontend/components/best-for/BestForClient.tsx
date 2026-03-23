"use client"

import { useState, useEffect, useCallback, useRef } from "react"
import FilterControls, { FilterState } from "./FilterControls"
import BestForResults, { RecommendedModel } from "./BestForResults"
import type { UseCase } from "@/lib/use-cases"

interface BestForClientProps {
  useCase: UseCase
}

const DEFAULT_FILTERS: FilterState = {
  priority: "balanced",
  maxPriceInput: null,
  contextMin: null,
  requiresTools: false,
  requiresStructuredOutput: false,
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "https://api.llmrates.live"
const API_KEY = process.env.NEXT_PUBLIC_API_KEY

function buildApiUrl(useCase: string, filters: FilterState): string {
  const params = new URLSearchParams({
    use_case: useCase,
    priority: filters.priority,
    top_k: "5",
  })
  if (filters.maxPriceInput != null) {
    // UI stores price in $/1M tokens; API expects price per single token.
    // Divide by 1_000_000 to convert: e.g. $1/1M → 0.000001 per token.
    params.set("max_price_input", String(filters.maxPriceInput / 1_000_000))
  }
  if (filters.contextMin != null) {
    params.set("context", String(filters.contextMin))
  }
  if (filters.requiresTools) params.set("requires_tools", "true")
  if (filters.requiresStructuredOutput) params.set("requires_structured_output", "true")
  return `${API_BASE}/v1/recommend?${params.toString()}`
}

export default function BestForClient({ useCase }: BestForClientProps) {
  const [filters, setFilters] = useState<FilterState>(DEFAULT_FILTERS)
  const [models, setModels] = useState<RecommendedModel[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const fetchModels = useCallback(async (uc: string, f: FilterState) => {
    setLoading(true)
    try {
      const headers: Record<string, string> = {}
      if (API_KEY) headers["Authorization"] = `Bearer ${API_KEY}`
      const res = await fetch(buildApiUrl(uc, f), { headers })
      if (!res.ok) throw new Error(`API error ${res.status}`)
      const json = await res.json()
      // Phase 1 (#145) changed the capability-aware recommend response to a
      // nested envelope: { data: { items: [...], warnings: [...] }, meta: {...} }
      // Read items from data.items (not data directly).
      const envelope = json.data as { items?: RecommendedModel[]; warnings?: string[] } | null
      setModels(envelope?.items ?? [])
      setWarnings(envelope?.warnings ?? [])
    } catch {
      setModels([])
      setWarnings([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Initial fetch on use-case change.
  useEffect(() => {
    fetchModels(useCase.slug, filters)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [useCase.slug])

  // Clear pending debounce timer on unmount to avoid stale callbacks.
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const handleFilterChange = useCallback(
    (f: FilterState) => {
      setFilters(f)
      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        fetchModels(useCase.slug, f)
      }, 300)
    },
    [useCase.slug, fetchModels]
  )

  return (
    <div>
      <FilterControls value={filters} onChange={handleFilterChange} />
      <BestForResults useCase={useCase.slug} models={models} loading={loading} warnings={warnings} />
    </div>
  )
}
