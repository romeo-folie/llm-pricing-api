"use client"

import { useState, useEffect, useCallback, useRef } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
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
  }, [selectedSlugs])

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

  // Resolve selected slugs to Model objects for the table
  const selectedModels = selectedSlugs
    .map((slug) => allModels.find((m) => m.slug === slug))
    .filter(Boolean) as Model[]

  return (
    <div>
      {/* Page header */}
      <div className="animate-wireframe-fade" style={{ marginBottom: "24px", animationDelay: "1.2s" }}>
        <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)" }}>
          Compare LLM Pricing
        </h1>
        <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
          Compare AI model costs, token prices, context, trust, and available benchmark scores side by side.
        </p>
      </div>

      {/* Model picker */}
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
        <div>
          <div
            className="font-orbitron text-xs tracking-wide"
            style={{ color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: "8px" }}
          >
            Add Models
          </div>
          <ModelPicker
            models={allModels}
            selected={selectedSlugs}
            onSelect={handleSelect}
            onRemove={handleRemove}
            max={5}
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
