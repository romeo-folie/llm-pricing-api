"use client"

import { useState, useCallback } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
import { trackCompareModels, trackShareComparison } from "@/lib/analytics"
import ModelPicker from "./ModelPicker"
import CompareTable from "./CompareTable"
import EmptyState from "@/components/ui/EmptyState"

interface CompareClientProps {
  allModels:      Model[]
  initialCompare: Model[]
  initialIds:     string[]
}

export default function CompareClient({
  allModels,
  initialCompare,
  initialIds,
}: CompareClientProps) {
  const router       = useRouter()
  const searchParams = useSearchParams()
  const [selectedIds,    setSelectedIds]    = useState<string[]>(initialIds)
  const [compareModels,  setCompareModels]  = useState<Model[]>(initialCompare)
  const [copied,         setCopied]         = useState(false)

  const updateUrl = useCallback((ids: string[]) => {
    const params = new URLSearchParams(searchParams.toString())
    if (ids.length > 0) {
      params.set("models", ids.join(","))
    } else {
      params.delete("models")
    }
    router.push(`?${params.toString()}`, { scroll: false })
  }, [router, searchParams])

  function handleSelect(id: string) {
    let next = [...selectedIds]
    if (next.includes(id)) return
    if (next.length >= 5) next = next.slice(1) // replace oldest
    next = [...next, id]
    setSelectedIds(next)
    const picked = next.map((i) => allModels.find((m) => m.id === i)).filter(Boolean) as Model[]
    setCompareModels(picked)
    updateUrl(next)
    if (next.length >= 2) trackCompareModels(next)
  }

  function handleRemove(id: string) {
    const next = selectedIds.filter((i) => i !== id)
    setSelectedIds(next)
    setCompareModels(compareModels.filter((m) => m.id !== id))
    updateUrl(next)
  }

  function handleShare() {
    navigator.clipboard.writeText(window.location.href).then(() => {
      setCopied(true)
      trackShareComparison(selectedIds)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }

  return (
    <div>
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: "12px",
          marginBottom: "24px",
        }}
      >
        <div>
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
            Select up to 5 models for side-by-side comparison
          </p>
        </div>

        {compareModels.length >= 2 && (
          <button
            onClick={handleShare}
            className="font-outfit text-sm"
            style={{
              padding: "8px 16px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--surface)",
              color: copied ? "var(--green)" : "var(--muted)",
              cursor: "pointer",
              transition: "color 0.2s",
            }}
          >
            {copied ? "✓ Copied!" : "Share ↗"}
          </button>
        )}
      </div>

      {/* Picker */}
      <div
        style={{
          padding: "16px",
          border: "1px solid var(--border)",
          backgroundColor: "var(--surfaceLo)",
          marginBottom: "24px",
        }}
      >
        <ModelPicker
          models={allModels}
          selected={selectedIds}
          onSelect={handleSelect}
          onRemove={handleRemove}
          max={5}
          placeholder="Search and add models to compare…"
        />
      </div>

      {/* Table or empty state */}
      {compareModels.length < 2 ? (
        <EmptyState padding="80px 24px">Select at least 2 models to compare.</EmptyState>
      ) : (
        <CompareTable models={compareModels} onRemove={handleRemove} />
      )}
    </div>
  )
}
