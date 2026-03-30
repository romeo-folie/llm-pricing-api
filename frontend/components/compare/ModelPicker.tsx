"use client"

import { useState, useRef, useEffect, useCallback } from "react"
import * as Popover from "@radix-ui/react-popover"
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
  const inputRef = useRef<HTMLInputElement>(null)
  const triggerRef = useRef<HTMLDivElement>(null)

  const atMax = selected.length >= max

  // Filter out already-selected, match query
  const filtered = models.filter((m) => {
    if (selected.includes(m.slug)) return false
    const q = query.toLowerCase()
    return !q || m.name.toLowerCase().includes(q) || m.provider.toLowerCase().includes(q)
  })

  // Soft-sort: scored models first when a use-case filter is active
  const sorted = scoredSlugs && scoredSlugs.size > 0
    ? [...filtered].sort((a, b) => {
        const aScored = scoredSlugs.has(a.slug) ? 0 : 1
        const bScored = scoredSlugs.has(b.slug) ? 0 : 1
        if (aScored !== bScored) return aScored - bScored
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

  const handleSelect = useCallback((slug: string) => {
    onSelect(slug)
    setQuery("")
    const newCount = selected.length + 1
    if (newCount >= max) {
      setOpen(false) // dismiss when max reached
    } else {
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [onSelect, selected.length, max])

  return (
    <div ref={triggerRef}>
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

      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Anchor asChild>
          {/* Wrapper gives us a positioned container for the caret overlay */}
          <div style={{ position: "relative" }}>
            <input
              ref={inputRef}
              type="text"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value)
                if (!open) setOpen(true)
              }}
              onFocus={() => setOpen(true)}
              onKeyDown={(e) => {
                if (e.key === "Escape") { setOpen(false); setQuery("") }
              }}
              placeholder={atMax ? `Max ${max} selected` : placeholder}
              disabled={atMax}
              className={`font-outfit ${styles.input}`}
              style={{ paddingRight: "28px" }}
              autoComplete="off"
            />
            {/* Caret — matches the filled-triangle used on selects across the app */}
            <div
              aria-hidden
              onClick={() => { if (!atMax) { setOpen(!open); inputRef.current?.focus() } }}
              style={{
                position: "absolute",
                right: "10px",
                top: "50%",
                transform: "translateY(-50%)",
                pointerEvents: "all",
                cursor: atMax ? "not-allowed" : "pointer",
                width: "12px",
                height: "12px",
                backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2378716C' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
                backgroundRepeat: "no-repeat",
                backgroundPosition: "center",
                transition: "transform 0.15s",
                ...(open ? { transform: "translateY(-50%) rotate(180deg)" } : {}),
              }}
            />
          </div>
        </Popover.Anchor>

        <Popover.Portal>
          <Popover.Content
            onOpenAutoFocus={(e) => e.preventDefault()} // don't steal focus from input
            onInteractOutside={(e) => {
              // Don't close if clicking the input itself
              if (inputRef.current?.contains(e.target as Node)) {
                e.preventDefault()
              }
            }}
            align="start"
            sideOffset={2}
            style={{
              width: "var(--radix-popover-trigger-width)",
              maxHeight: "260px",
              overflowY: "auto",
              border: "1px solid var(--borderDk)",
              borderTop: "none",
              backgroundColor: "var(--bg)",
              zIndex: 9999,
              borderRadius: 0,
              padding: 0,
              boxShadow: "0 4px 12px rgba(0,0,0,0.12)",
            }}
          >
            {sorted.length > 0 ? (
              <ul style={{ margin: 0, padding: 0, listStyle: "none" }}>
                {sorted.slice(0, 40).map((m, i) => (
                  <li key={m.slug}>
                    {dividerIdx > 0 && i === dividerIdx && (
                      <div
                        className="font-outfit text-xs"
                        style={{
                          padding: "5px 12px",
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
                      onPointerDown={(e) => {
                        e.preventDefault()
                        handleSelect(m.slug)
                      }}
                      className={`font-outfit ${styles.option}`}
                    >
                      <span>{m.name}</span>
                      <SiteBadge label={m.provider} {...providerStyle(m.provider)} />
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <div
                className="font-outfit text-sm"
                style={{ padding: "12px", color: "var(--muted)", fontStyle: "italic" }}
              >
                No models found
              </div>
            )}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  )
}
