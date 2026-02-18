"use client"

import { useState, useCallback, useId } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
import ModelPicker from "./ModelPicker"
import CompareTable from "./CompareTable"

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
  const emptyGridId  = useId()

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
          <h1 className="font-orbitron text-2xl font-bold" style={{ color: "var(--ink)" }}>
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
              borderRadius: "2px",
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
          borderRadius: "2px",
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
        <div
          className="font-outfit text-sm"
          style={{
            position: "relative",
            textAlign: "center",
            padding: "80px 24px",
            border: "1px solid var(--border)",
            color: "var(--muted)",
            overflow: "hidden",
          }}
        >
          <svg aria-hidden="true" style={{ position: "absolute", inset: 0, width: "100%", height: "100%", opacity: 0.06, pointerEvents: "none" }}>
            <defs>
              <pattern id={emptyGridId} x="0" y="0" width="40" height="40" patternUnits="userSpaceOnUse" patternTransform="rotate(-30) skewX(20)">
                <path d="M 40 0 L 0 0 0 40" fill="none" stroke="var(--borderDk)" strokeWidth="0.5" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill={`url(#${emptyGridId})`} />
          </svg>
          <span style={{ position: "relative" }}>Select at least 2 models to compare.</span>
        </div>
      ) : (
        <CompareTable models={compareModels} onRemove={handleRemove} />
      )}
    </div>
  )
}
