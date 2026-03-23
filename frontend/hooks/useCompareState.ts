"use client"

import { useCallback, useMemo } from "react"
import { useRouter, useSearchParams } from "next/navigation"

export interface CompareFilters {
  priority: "quality" | "cost" | "speed" | "balanced"
  maxPriceInput: number | null
  contextMin: number | null
  requiresTools: boolean
  requiresStructuredOutput: boolean
}

export interface CompareState {
  useCase: string | null
  filters: CompareFilters
  selectedModels: string[] // slugs, max 2
}

const DEFAULT_FILTERS: CompareFilters = {
  priority: "balanced",
  maxPriceInput: null,
  contextMin: null,
  requiresTools: false,
  requiresStructuredOutput: false,
}

function parseSearchParams(sp: URLSearchParams): CompareState {
  const useCase = sp.get("use-case") || null
  const priority = sp.get("priority")
  const maxPrice = sp.get("max-price")
  const context = sp.get("context")
  const models = sp.get("models")

  return {
    useCase,
    filters: {
      priority:
        priority === "quality" || priority === "cost" || priority === "speed"
          ? priority
          : "balanced",
      maxPriceInput: maxPrice ? Number(maxPrice) : null,
      contextMin: context ? Number(context) : null,
      requiresTools: sp.get("requires-tools") === "true",
      requiresStructuredOutput: sp.get("requires-json") === "true",
    },
    selectedModels: models ? models.split(",").filter(Boolean).slice(0, 2) : [],
  }
}

function buildSearchParams(state: CompareState): URLSearchParams {
  const params = new URLSearchParams()
  if (state.useCase) params.set("use-case", state.useCase)
  if (state.filters.priority !== "balanced")
    params.set("priority", state.filters.priority)
  if (state.filters.maxPriceInput != null)
    params.set("max-price", String(state.filters.maxPriceInput))
  if (state.filters.contextMin != null)
    params.set("context", String(state.filters.contextMin))
  if (state.filters.requiresTools) params.set("requires-tools", "true")
  if (state.filters.requiresStructuredOutput)
    params.set("requires-json", "true")
  if (state.selectedModels.length > 0)
    params.set("models", state.selectedModels.join(","))
  return params
}

export function useCompareState() {
  const router = useRouter()
  const searchParams = useSearchParams()

  const state = useMemo(
    () => parseSearchParams(searchParams),
    [searchParams],
  )

  const setUseCase = useCallback(
    (slug: string | null) => {
      const next: CompareState = {
        ...state,
        useCase: slug,
        // Reset selected models when switching use case.
        selectedModels: [],
      }
      const qs = buildSearchParams(next).toString()
      router.push(`/compare${qs ? `?${qs}` : ""}`)
    },
    [router, state],
  )

  const setFilters = useCallback(
    (filters: CompareFilters) => {
      const next: CompareState = { ...state, filters }
      const qs = buildSearchParams(next).toString()
      router.replace(`/compare${qs ? `?${qs}` : ""}`, { scroll: false })
    },
    [router, state],
  )

  const setSelectedModels = useCallback(
    (models: string[]) => {
      const next: CompareState = { ...state, selectedModels: models.slice(0, 2) }
      const qs = buildSearchParams(next).toString()
      router.replace(`/compare${qs ? `?${qs}` : ""}`, { scroll: false })
    },
    [router, state],
  )

  const toggleModel = useCallback(
    (slug: string) => {
      const current = state.selectedModels
      let next: string[]
      if (current.includes(slug)) {
        next = current.filter((s) => s !== slug)
      } else if (current.length >= 2) {
        // Replace oldest selection.
        next = [current[1], slug]
      } else {
        next = [...current, slug]
      }
      setSelectedModels(next)
    },
    [state.selectedModels, setSelectedModels],
  )

  return {
    ...state,
    setUseCase,
    setFilters,
    setSelectedModels,
    toggleModel,
  }
}
