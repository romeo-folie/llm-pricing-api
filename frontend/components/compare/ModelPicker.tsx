"use client"

import { useState, useRef, useEffect } from "react"
import type { Model } from "@/lib/api"
import styles from "./ModelPicker.module.css"

interface ModelPickerProps {
  models: Model[]
  selected: string[]
  onSelect: (id: string) => void
  onRemove: (id: string) => void
  max?: number
  placeholder?: string
}

export default function ModelPicker({
  models,
  selected,
  onSelect,
  onRemove,
  max = 5,
  placeholder = "Search models…",
}: ModelPickerProps) {
  const [query, setQuery] = useState("")
  const [open, setOpen]   = useState(false)
  const containerRef       = useRef<HTMLDivElement>(null)

  const filtered = models.filter((m) => {
    if (selected.includes(m.id)) return false
    const q = query.toLowerCase()
    return m.name.toLowerCase().includes(q) || m.provider.toLowerCase().includes(q)
  })

  const selectedModels = selected
    .map((id) => models.find((m) => m.id === id))
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
            <span key={m.id} className={`font-outfit ${styles.chip}`}>
              {m.name}
              <button
                onClick={() => onRemove(m.id)}
                aria-label={`Remove ${m.name}`}
                className={styles.chipRemove}
              >
                ×
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
      {open && !atMax && filtered.length > 0 && (
        <ul className={styles.dropdown}>
          {filtered.slice(0, 20).map((m) => (
            <li key={m.id}>
              <button
                onMouseDown={(e) => {
                  e.preventDefault()
                  onSelect(m.id)
                  setQuery("")
                  setOpen(false)
                }}
                className={`font-outfit ${styles.option}`}
              >
                <span>{m.name}</span>
                <span className={styles.providerBadge}>{m.provider}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
