"use client"

import { useState, useRef, useEffect } from "react"
import type { Model } from "@/lib/api"
import { providerStyle } from "@/lib/provider-colors"
import { SiteBadge } from "@/components/ui/SiteBadge"
import styles from "./ModelPicker.module.css"

interface ModelPickerProps {
  models: Model[]
  selected: string[]
  onSelect: (slug: string) => void
  onRemove: (slug: string) => void
  max?: number
  placeholder?: string
  /** Slugs of models that have benchmark data for the active use-case. */
  scoredSlugs?: Set<string>
}

export default function ModelPicker({
  models,
  selected,
  onSelect,
  onRemove,
  max = 5,
  placeholder = "Search models\u2026",
  scoredSlugs,
}: ModelPickerProps) {
  const [query, setQuery] = useState("")
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  // Filter out already-selected models, then match query
  const filtered = models.filter((m) => {
    if (selected.includes(m.slug)) return false
    const q = query.toLowerCase()
    return m.name.toLowerCase().includes(q) || m.provider.toLowerCase().includes(q)
  })

  // Soft-sort: scored models first when a use-case filter is active
  const sorted = scoredSlugs && scoredSlugs.size > 0
    ? [...filtered].sort((a, b) => {
        const aScored = scoredSlugs.has(a.slug) ? 0 : 1
        const bScored = scoredSlugs.has(b.slug) ? 0 : 1
        if (aScored !== bScored) return aScored - bScored
        // Secondary: alphabetical by provider then name
        const pCmp = a.provider.localeCompare(b.provider)
        return pCmp !== 0 ? pCmp : a.name.localeCompare(b.name)
      })
    : filtered

  // Find divider index (first unscored model)
  const dividerIdx = scoredSlugs && scoredSlugs.size > 0
    ? sorted.findIndex((m) => !scoredSlugs.has(m.slug))
    : -1

  const selectedModels = selected
    .map((slug) => models.find((m) => m.slug === slug))
    .filter(Boolean) as Model[]

  useEffect(() => {
    function handler(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [])

  const atMax = selected.length >= max

  return (
    <div ref={containerRef} style={{ position: "relative" }}>
      {/* Selected chips */}
      {selectedModels.length > 0 && (
        <div className={styles.chips}>
          {selectedModels.map((m) => (
            <span key={m.slug} className={`font-outfit ${styles.chip}`}>
              {m.name}
              <button
                onClick={() => onRemove(m.slug)}
                aria-label={`Remove ${m.name}`}
                className={styles.chipRemove}
              >
                &times;
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Search input */}
      <input
        type="text"
        value={query}
        onChange={(e) => { setQuery(e.target.value); setOpen(true) }}
        onFocus={() => setOpen(true)}
        placeholder={atMax ? `Max ${max} selected` : placeholder}
        disabled={atMax}
        className={`font-outfit ${styles.input}`}
      />

      {/* Dropdown */}
      {open && !atMax && sorted.length > 0 && (
        <ul className={styles.dropdown}>
          {sorted.slice(0, 30).map((m, i) => (
            <li key={m.slug}>
              {/* Divider between scored and unscored */}
              {dividerIdx > 0 && i === dividerIdx && (
                <div
                  className="font-outfit text-xs"
                  style={{
                    padding: "6px 12px",
                    color: "var(--dim)",
                    backgroundColor: "var(--surfaceLo)",
                    borderBottom: "1px solid var(--border)",
                    fontStyle: "italic",
                  }}
                >
                  No benchmark data
                </div>
              )}
              <button
                onMouseDown={(e) => {
                  e.preventDefault()
                  onSelect(m.slug)
                  setQuery("")
                  setOpen(false)
                }}
                className={`font-outfit ${styles.option}`}
              >
                <span>{m.name}</span>
                <SiteBadge
                  label={m.provider}
                  {...providerStyle(m.provider)}
                />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
