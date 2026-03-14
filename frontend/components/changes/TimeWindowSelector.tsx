"use client"

import type { TimeWindow } from "@/lib/changes-aggregation"

interface TimeWindowSelectorProps {
  value: TimeWindow
  onChange: (w: TimeWindow) => void
}

const WINDOWS: { label: string; value: TimeWindow }[] = [
  { label: "24H",  value: "24h" },
  { label: "7D",   value: "7d"  },
  { label: "30D",  value: "30d" },
]

export default function TimeWindowSelector({ value, onChange }: TimeWindowSelectorProps) {
  return (
    <div style={{ display: "flex", gap: "4px", marginBottom: "16px" }}>
      {WINDOWS.map((w) => {
        const active = value === w.value
        return (
          <button
            key={w.value}
            onClick={() => onChange(w.value)}
            className="font-orbitron text-xs"
            style={{
              padding: "4px 12px",
              border: `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
              backgroundColor: active ? "var(--accent)" : "var(--bg)",
              color: active ? "white" : "var(--muted)",
              cursor: "pointer",
              letterSpacing: "0.05em",
              transition: "background-color 0.15s, color 0.15s, border-color 0.15s",
            }}
          >
            {w.label}
          </button>
        )
      })}
    </div>
  )
}
