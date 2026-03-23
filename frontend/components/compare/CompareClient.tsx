"use client"

import { useState, useEffect, useCallback, useRef } from "react"
import { useCompareState } from "@/hooks/useCompareState"
import type { CompareFilters } from "@/hooks/useCompareState"
import UseCasePicker from "./UseCasePicker"
import FilterControls from "./FilterControls"
import ResultsPanel from "./ResultsPanel"
import type { RecommendedModel } from "./ResultsPanel"
import ComparePanel from "./ComparePanel"
import type { CompareModel } from "./ComparePanel"

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "https://api.llmrates.live"
const API_KEY = process.env.NEXT_PUBLIC_API_KEY

function buildRecommendUrl(useCase: string, filters: CompareFilters): string {
  const params = new URLSearchParams({
    use_case: useCase,
    priority: filters.priority,
    top_k: "5",
  })
  if (filters.maxPriceInput != null) {
    params.set("max_price_input", String(filters.maxPriceInput / 1_000_000))
  }
  if (filters.contextMin != null) {
    params.set("context", String(filters.contextMin))
  }
  if (filters.requiresTools) params.set("requires_tools", "true")
  if (filters.requiresStructuredOutput) params.set("requires_structured_output", "true")
  return `${API_BASE}/v1/recommend?${params.toString()}`
}

function buildCompareUrl(slugs: string[], useCase: string | null): string {
  const params = new URLSearchParams({ models: slugs.join(",") })
  if (useCase) params.set("use_case", useCase)
  return `${API_BASE}/v1/compare?${params.toString()}`
}

function buildHeaders(): Record<string, string> {
  const headers: Record<string, string> = {}
  if (API_KEY) headers["Authorization"] = `Bearer ${API_KEY}`
  return headers
}

export default function CompareClient() {
  const {
    useCase,
    filters,
    selectedModels,
    setUseCase,
    setFilters,
    toggleModel,
    setSelectedModels,
  } = useCompareState()

  const [recommendations, setRecommendations] = useState<RecommendedModel[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [apiError, setApiError] = useState<string | null>(null)
  const [compareData, setCompareData] = useState<CompareModel[] | null>(null)
  const [compareLoading, setCompareLoading] = useState(false)
  const [compareError, setCompareError] = useState<string | null>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const comparePanelRef = useRef<HTMLDivElement>(null)

  // Fetch recommendations when useCase or filters change.
  const fetchRecommendations = useCallback(async (uc: string, f: CompareFilters) => {
    setLoading(true)
    setApiError(null)
    try {
      const res = await fetch(buildRecommendUrl(uc, f), { headers: buildHeaders() })
      if (!res.ok) throw new Error(`API error ${res.status}`)
      const json = await res.json()
      const envelope = json.data as { items?: RecommendedModel[]; warnings?: string[] } | null
      setRecommendations(envelope?.items ?? [])
      setWarnings(envelope?.warnings ?? [])
    } catch (err) {
      setRecommendations([])
      setWarnings([])
      setApiError(err instanceof Error ? err.message : "Failed to load recommendations")
    } finally {
      setLoading(false)
    }
  }, [])

  // Fetch compare data when 2 models are selected.
  const fetchCompareData = useCallback(async (slugs: string[], uc: string | null) => {
    setCompareLoading(true)
    setCompareError(null)
    try {
      const res = await fetch(buildCompareUrl(slugs, uc), { headers: buildHeaders() })
      if (!res.ok) throw new Error(`API error ${res.status}`)
      const json = await res.json()
      const envelope = json.data as { items?: CompareModel[] } | null
      setCompareData(envelope?.items ?? null)
    } catch (err) {
      setCompareData(null)
      setCompareError(err instanceof Error ? err.message : "Failed to load comparison data")
    } finally {
      setCompareLoading(false)
    }
  }, [])

  // Initial fetch and refetch on use-case change.
  useEffect(() => {
    if (useCase) {
      fetchRecommendations(useCase, filters)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [useCase])

  // Debounced filter changes.
  const handleFilterChange = useCallback(
    (f: CompareFilters) => {
      setFilters(f)
      if (!useCase) return
      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        fetchRecommendations(useCase, f)
      }, 250)
    },
    [useCase, fetchRecommendations, setFilters],
  )

  // Clear debounce on unmount.
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  // Fetch compare data when 2 models are selected.
  useEffect(() => {
    if (selectedModels.length === 2) {
      fetchCompareData(selectedModels, useCase)
    } else {
      setCompareData(null)
    }
  }, [selectedModels, useCase, fetchCompareData])

  // Auto-scroll to compare panel when data arrives.
  useEffect(() => {
    if (compareData && compareData.length === 2 && comparePanelRef.current) {
      comparePanelRef.current.scrollIntoView({ behavior: "smooth", block: "start" })
    }
  }, [compareData])

  const handleSwap = useCallback(
    (position: "a" | "b", newSlug: string) => {
      const next = [...selectedModels]
      if (position === "a") next[0] = newSlug
      else next[1] = newSlug
      setSelectedModels(next)
    },
    [selectedModels, setSelectedModels],
  )

  const handleClear = useCallback(() => {
    setSelectedModels([])
    setCompareData(null)
  }, [setSelectedModels])

  return (
    <div>
      {/* Page header */}
      <div className="animate-wireframe-fade" style={{ marginBottom: "24px", animationDelay: "1.2s" }}>
        <span
          className="font-orbitron text-xs tracking-widest"
          style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
        >
          COMPARE
        </span>
        <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)" }}>
          Compare AI Models
        </h1>
        <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
          Choose a use case, explore top-ranked models, then select two to compare side by side.
        </p>
      </div>

      {/* 1. UseCasePicker — always visible */}
      <div className="mb-6 animate-wireframe-fade" style={{ animationDelay: "1.4s" }}>
        <div className="text-xs uppercase tracking-wide mb-3 font-semibold" style={{ color: "var(--muted)" }}>
          What are you building?
        </div>
        <UseCasePicker selected={useCase} onChange={setUseCase} />
      </div>

      {/* 2. FilterControls — visible when useCase selected */}
      {useCase && (
        <div className="animate-wireframe-fade">
          <FilterControls value={filters} onChange={handleFilterChange} />
        </div>
      )}

      {/* API error banner — shown when recommendations fetch fails */}
      {apiError && !loading && (
        <div
          className="mb-4 text-sm px-4 py-3 border"
          style={{ borderColor: "var(--warning-border, #B45309)", color: "var(--warning-text, #B45309)", backgroundColor: "var(--warning-bg, #FEF3C7)" }}
          role="alert"
        >
          ⚠ Unable to load recommendations: {apiError}
        </div>
      )}

      {/* 3. ResultsPanel — visible when useCase selected */}
      {useCase && (
        <div className="animate-wireframe-fade">
          <ResultsPanel
            useCase={useCase}
            models={recommendations}
            loading={loading}
            warnings={warnings}
            selectedModels={selectedModels}
            onSelect={toggleModel}
          />
        </div>
      )}

      {/* 4. ComparePanel — visible when 2 models selected */}
      {selectedModels.length === 2 && (
        <div ref={comparePanelRef}>
          {compareError && !compareLoading && (
            <div
              className="mb-4 text-sm px-4 py-3 border"
              style={{ borderColor: "var(--warning-border, #B45309)", color: "var(--warning-text, #B45309)", backgroundColor: "var(--warning-bg, #FEF3C7)" }}
              role="alert"
            >
              ⚠ Unable to load comparison: {compareError}
            </div>
          )}
          {compareLoading || !compareData || compareData.length < 2 ? (
            <ComparePanel
              modelA={undefined}
              modelB={undefined}
              useCase={useCase}
              availableModels={recommendations}
              onSwap={handleSwap}
              onClear={handleClear}
              loading={true}
            />
          ) : (
            <ComparePanel
              modelA={compareData[0]}
              modelB={compareData[1]}
              useCase={useCase}
              availableModels={recommendations}
              onSwap={handleSwap}
              onClear={handleClear}
            />
          )}
        </div>
      )}
    </div>
  )
}
