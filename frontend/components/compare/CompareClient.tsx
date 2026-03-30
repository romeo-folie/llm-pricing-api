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

  // Keep selectedSlugs in sync with URL on browser back/forward navigation.
  // We compare the URL value to the last value WE pushed — if they differ, the
  // navigation came from outside (history pop) and we should update state.
  const lastPushedUrl = useRef(initialModels.join(","))
  useEffect(() => {
    const fromUrl = searchParams.get("models")?.split(",").filter(Boolean).slice(0, 5) ?? []
    const fromUrlKey = fromUrl.join(",")
    if (fromUrlKey !== lastPushedUrl.current) {
      // URL changed externally (back/forward) — pull state from URL.
      lastPushedUrl.current = fromUrlKey
      setSelectedSlugs(fromUrl)
    }
  }, [searchParams])
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
      const key = slugs.join(",")
      lastPushedUrl.current = key
      const params = new URLSearchParams()
      if (slugs.length > 0) params.set("models", key)
      const qs = params.toString()
      router.replace(`/compare${qs ? `?${qs}` : ""}`, { scroll: false })
    },
    [router],
  )

  const handleSelect = useCallback(
    (slug: string) => {
      setSelectedSlugs((prev) => {
        if (prev.includes(slug) || prev.length >= 5) return prev
        return [...prev, slug]
      })
    },
    [],
  )

  const handleRemove = useCallback(
    (slug: string) => {
      setSelectedSlugs((prev) => prev.filter((s) => s !== slug))
    },
    [],
  )

  // Sync URL whenever selectedSlugs changes — outside the state updater to
  // avoid side effects inside a pure updater (would fire twice in Strict Mode).
  useEffect(() => {
    syncUrl(selectedSlugs)
  }, [selectedSlugs, syncUrl])

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
        <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)" }}>
          Compare Models
        </h1>
        <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
          Pick up to 5 models to compare side by side. Optionally filter by use case to see benchmark scores.
        </p>
      </div>

      {/* Controls card — FilterBar + ModelPicker */}
      <div
        className="animate-draw-border-box animate-wireframe-fade"
        style={{
          padding: "20px",
          backgroundColor: "var(--surfaceLo)",
          marginBottom: "24px",
          animationDelay: "1.3s",
          "--draw-delay": "1.3s",
          display: "flex",
          flexDirection: "column",
          gap: "16px",
        } as React.CSSProperties}
      >
        <FilterBar
          useCase={useCase}
          onUseCaseChange={setUseCase}
          requiresTools={requiresTools}
          onRequiresToolsChange={setRequiresTools}
          requiresStructuredOutput={requiresStructuredOutput}
          onRequiresStructuredOutputChange={setRequiresStructuredOutput}
        />

        <div>
          <div
            className="font-orbitron text-xs tracking-wide"
            style={{ color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: "8px" }}
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
          {selectedSlugs.length > 0 && (
            <div
              className="font-outfit text-xs"
              style={{ color: "var(--muted)", marginTop: "8px" }}
            >
              {selectedSlugs.length} of 5 selected
              {selectedSlugs.length >= 2 && (
                <span style={{ color: "var(--accent)", marginLeft: "8px" }}>↓ comparison below</span>
              )}
            </div>
          )}
        </div>
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
