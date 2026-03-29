"use client"

import { useState, useEffect, useCallback, useRef } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
import FilterBar from "./FilterBar"
import ModelPicker from "./ModelPicker"
import CompareTable from "./CompareTable"
import type { CompareScores } from "./CompareTable"

interface CompareClientProps {
  allModels: Model[]
}

interface CompareApiItem {
  slug: string
  scores?: Record<string, number>
}

export default function CompareClient({ allModels }: CompareClientProps) {
  const router = useRouter()
  const searchParams = useSearchParams()

  // ── State from URL ──────────────────────────────────────────────────────────
  const initialModels = searchParams.get("models")?.split(",").filter(Boolean).slice(0, 5) ?? []

  const [selectedSlugs, setSelectedSlugs] = useState<string[]>(initialModels)
  const [useCase, setUseCase] = useState<string | null>(null)
  const [contextMin, setContextMin] = useState<number | null>(null)
  const [requiresTools, setRequiresTools] = useState(false)
  const [requiresStructuredOutput, setRequiresStructuredOutput] = useState(false)

  // ── Scored slugs for soft-sort in picker ────────────────────────────────────
  const [scoredSlugs, setScoredSlugs] = useState<Set<string>>(new Set())
  const recAbort = useRef<AbortController | null>(null)

  // Fetch recommend when useCase changes, to know which models have scores
  useEffect(() => {
    if (!useCase) {
      setScoredSlugs(new Set())
      return
    }
    recAbort.current?.abort()
    const ctrl = new AbortController()
    recAbort.current = ctrl
    fetch(`/api/recommend?use_case=${encodeURIComponent(useCase)}&top_k=50`, {
      signal: ctrl.signal,
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((json) => {
        if (!json) return
        const items = (json?.data?.items ?? []) as { slug: string }[]
        setScoredSlugs(new Set(items.map((i) => i.slug)))
      })
      .catch(() => {
        /* abort or network error — fall back to unfiltered */
      })
    return () => ctrl.abort()
  }, [useCase])

  // ── Compare scores (benchmark data) ────────────────────────────────────────
  const [compareScores, setCompareScores] = useState<CompareScores>({})
  const cmpAbort = useRef<AbortController | null>(null)

  useEffect(() => {
    if (selectedSlugs.length < 2) {
      setCompareScores({})
      return
    }
    cmpAbort.current?.abort()
    const ctrl = new AbortController()
    cmpAbort.current = ctrl
    const params = new URLSearchParams({ models: selectedSlugs.join(",") })
    if (useCase) params.set("use_case", useCase)
    fetch(`/api/compare?${params.toString()}`, { signal: ctrl.signal })
      .then((r) => (r.ok ? r.json() : null))
      .then((json) => {
        if (!json) return
        const items = (json?.data?.items ?? []) as CompareApiItem[]
        const map: CompareScores = {}
        for (const item of items) {
          if (item.scores && Object.keys(item.scores).length > 0) {
            map[item.slug] = item.scores
          }
        }
        setCompareScores(map)
      })
      .catch(() => {
        /* abort or network error */
      })
    return () => ctrl.abort()
  }, [selectedSlugs, useCase])

  // ── URL sync ───────────────────────────────────────────────────────────────
  const syncUrl = useCallback(
    (slugs: string[]) => {
      const params = new URLSearchParams()
      if (slugs.length > 0) params.set("models", slugs.join(","))
      const qs = params.toString()
      router.replace(`/compare${qs ? `?${qs}` : ""}`, { scroll: false })
    },
    [router],
  )

  const handleSelect = useCallback(
    (slug: string) => {
      setSelectedSlugs((prev) => {
        if (prev.includes(slug) || prev.length >= 5) return prev
        const next = [...prev, slug]
        syncUrl(next)
        return next
      })
    },
    [syncUrl],
  )

  const handleRemove = useCallback(
    (slug: string) => {
      setSelectedSlugs((prev) => {
        const next = prev.filter((s) => s !== slug)
        syncUrl(next)
        return next
      })
    },
    [syncUrl],
  )

  // ── Client-side filters on the model list ─────────────────────────────────
  const filteredModels = allModels.filter((m) => {
    if (contextMin != null && m.context_window < contextMin) return false
    // Tool calling and structured output: the Model type doesn't have these
    // fields yet, so the checkboxes are UI-only placeholders for now.
    return true
  })

  // Resolve selected slugs to Model objects for the table
  const selectedModels = selectedSlugs
    .map((slug) => allModels.find((m) => m.slug === slug))
    .filter(Boolean) as Model[]

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
          Compare Models
        </h1>
        <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
          Pick up to 5 models to compare side by side. Optionally filter by use case to see benchmark scores.
        </p>
      </div>

      {/* FilterBar */}
      <div style={{ marginBottom: "16px" }}>
        <FilterBar
          useCase={useCase}
          onUseCaseChange={setUseCase}
          contextMin={contextMin}
          onContextMinChange={setContextMin}
          requiresTools={requiresTools}
          onRequiresToolsChange={setRequiresTools}
          requiresStructuredOutput={requiresStructuredOutput}
          onRequiresStructuredOutputChange={setRequiresStructuredOutput}
        />
      </div>

      {/* ModelPicker */}
      <div className="animate-wireframe-fade" style={{ marginBottom: "24px", animationDelay: "1.5s" }}>
        <div
          className="font-orbitron text-xs tracking-wide mb-2"
          style={{ color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.08em" }}
        >
          Add Models
        </div>
        <ModelPicker
          models={filteredModels}
          selected={selectedSlugs}
          onSelect={handleSelect}
          onRemove={handleRemove}
          max={5}
          scoredSlugs={scoredSlugs.size > 0 ? scoredSlugs : undefined}
        />
      </div>

      {/* CompareTable — renders when 2+ models selected */}
      {selectedModels.length >= 2 && (
        <div className="animate-wireframe-fade" style={{ animationDelay: "0.2s" }}>
          <CompareTable
            models={selectedModels}
            onRemove={handleRemove}
            scores={Object.keys(compareScores).length > 0 ? compareScores : undefined}
          />
        </div>
      )}

      {/* Empty state prompt */}
      {selectedModels.length < 2 && (
        <div
          className="animate-wireframe-fade font-outfit text-sm"
          style={{
            textAlign: "center",
            color: "var(--dim)",
            padding: "48px 16px",
            border: "1px dashed var(--border)",
            animationDelay: "1.6s",
          }}
        >
          {selectedModels.length === 0
            ? "Search and select at least 2 models above to start comparing."
            : "Select one more model to see the comparison table."}
        </div>
      )}
    </div>
  )
}
