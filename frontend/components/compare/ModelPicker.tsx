"use client"

import { useState, useRef, useEffect } from "react"
import type { Model } from "@/lib/api"

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
      {selectedModels.length > 0 && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", marginBottom: "8px" }}>
          {selectedModels.map((m) => (
            <span
              key={m.id}
              className="font-outfit text-xs"
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "4px",
                padding: "2px 8px",
                border: "1px solid var(--borderDk)",


                backgroundColor: "var(--surfaceLo)",
                color: "var(--text)",
              }}
            >
              {m.name}
              <button
                onClick={() => onRemove(m.id)}
                aria-label={`Remove ${m.name}`}
                style={{ background: "none", border: "none", cursor: "pointer", color: "var(--muted)", padding: "0 2px", lineHeight: 1 }}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      <input
        type="text"
        value={query}
        onChange={(e) => { setQuery(e.target.value); setOpen(true) }}
        onFocus={() => setOpen(true)}
        placeholder={atMax ? `Max ${max} selected` : placeholder}
        disabled={atMax}
        className="font-outfit text-sm w-full"
        style={{
          padding: "8px 12px",
          border: "1px solid var(--border)",
          backgroundColor: atMax ? "var(--surfaceLo)" : "var(--surface)",
          color: "var(--text)",
          outline: "none",
          width: "100%",
        }}
      />

      {open && !atMax && filtered.length > 0 && (
        <ul
          style={{
            position: "absolute",
            top: "100%",
            left: 0,
            right: 0,
            zIndex: 50,
            border: "1px solid var(--borderDk)",
            borderTop: "none",
            backgroundColor: "var(--surface)",
            maxHeight: "240px",
            overflowY: "auto",
            margin: 0,
            padding: 0,
            listStyle: "none",
          }}
        >
          {filtered.slice(0, 20).map((m) => (
            <li key={m.id}>
              <button
                onMouseDown={(e) => { e.preventDefault(); onSelect(m.id); setQuery(""); setOpen(false) }}
                className="font-outfit text-sm"
                style={{
                  width: "100%",
                  textAlign: "left",
                  padding: "8px 12px",
                  background: "none",
                  border: "none",
                  borderBottom: "1px solid var(--border)",
                  cursor: "pointer",
                  color: "var(--text)",
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "center",
                }}
                onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = "var(--surfaceLo)")}
                onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
              >
                <span>{m.name}</span>
                <span className="font-outfit text-xs" style={{ color: "var(--muted)", padding: "1px 6px", border: "1px solid var(--border)" }}>
                  {m.provider}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
