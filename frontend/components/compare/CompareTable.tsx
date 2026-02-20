"use client"

import type { Model } from "@/lib/api"
import { formatPrice, formatContext, formatAge, formatSourceName } from "@/lib/format"

interface CompareTableProps {
  models: Model[]
  onRemove: (id: string) => void
}

const ROWS: { label: string; key: keyof Model | string; render: (m: Model) => string }[] = [
  { label: "Provider",      key: "provider",       render: (m) => m.provider },
  { label: "Input / 1M",   key: "input_price",    render: (m) => formatPrice(m.input_price_per_m) },
  { label: "Output / 1M",  key: "output_price",   render: (m) => formatPrice(m.output_price_per_m) },
  { label: "Context",      key: "context_window", render: (m) => formatContext(m.context_window) },
  { label: "Modality",     key: "modality",       render: (m) => m.modality },
  { label: "Confidence",   key: "confidence",     render: (m) => m.trust.confidence.toUpperCase() },
  { label: "Updated",      key: "updated",        render: (m) => formatAge(m.trust.age_hours) },
  { label: "Source",       key: "source",         render: (m) => formatSourceName(m.trust.source) },
]

export default function CompareTable({ models, onRemove }: CompareTableProps) {
  if (models.length < 2) return null

  // Best value = lowest combined price
  const bestId = models.reduce((best, m) => {
    const total = m.input_price_per_m + m.output_price_per_m
    const bestTotal = best.input_price_per_m + best.output_price_per_m
    return total < bestTotal ? m : best
  }).id

  const numericRows = new Set(["input_price", "output_price"])
  const isOrbitron  = (key: string) => numericRows.has(key) || key === "context_window"

  return (
    <div style={{ overflowX: "auto" }}>
      <table style={{ width: "100%", borderCollapse: "collapse" }}>
        {/* Model header row */}
        <thead>
          <tr>
            <th
              className="font-orbitron text-xs"
              style={{
                padding: "10px 12px",
                textAlign: "left",
                color: "var(--dim)",
                borderBottom: "2px solid var(--borderDk)",
                backgroundColor: "var(--surfaceLo)",
                width: "120px",
                textTransform: "uppercase",
                letterSpacing: "0.08em",
              }}
            >
              Feature
            </th>
            {models.map((m) => {
              const isBest = m.id === bestId
              return (
                <th
                  key={m.id}
                  style={{
                    padding: "10px 12px",
                    borderBottom: "2px solid var(--borderDk)",
                    borderLeft: isBest ? "3px solid var(--accent)" : "1px solid var(--border)",
                    backgroundColor: isBest ? "var(--accentLt)" : "var(--surface)",
                    position: "relative",
                  }}
                >
                  <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: "8px" }}>
                    <div>
                      {isBest && (
                        <div
                          className="font-orbitron text-xs"
                          style={{ color: "var(--accent)", marginBottom: "4px", letterSpacing: "0.06em" }}
                        >
                          ★ BEST VALUE
                        </div>
                      )}
                      <div className="font-outfit text-sm" style={{ color: "var(--ink)", fontWeight: 600 }}>
                        {m.name}
                      </div>
                    </div>
                    <button
                      onClick={() => onRemove(m.id)}
                      aria-label={`Remove ${m.name}`}
                      className="font-outfit text-xs"
                      style={{
                        background: "none",
                        border: "1px solid var(--border)",


                        color: "var(--dim)",
                        cursor: "pointer",
                        padding: "1px 5px",
                        lineHeight: 1.4,
                      }}
                    >
                      ×
                    </button>
                  </div>
                </th>
              )
            })}
          </tr>
        </thead>

        {/* Data rows */}
        <tbody>
          {ROWS.map((row, i) => (
            <tr key={row.label} style={{ backgroundColor: i % 2 === 0 ? "var(--surface)" : "var(--surfaceLo)" }}>
              <td
                className="font-outfit text-xs"
                style={{
                  padding: "10px 12px",
                  color: "var(--dim)",
                  borderBottom: "1px solid var(--border)",
                  borderRight: "1px solid var(--border)",
                  fontWeight: 500,
                  textTransform: "uppercase",
                  letterSpacing: "0.06em",
                  whiteSpace: "nowrap",
                }}
              >
                {row.label}
              </td>
              {models.map((m) => {
                const isBest = m.id === bestId
                return (
                  <td
                    key={m.id}
                    className={isOrbitron(row.key) ? "font-orbitron text-sm" : "font-outfit text-sm"}
                    style={{
                      padding: "10px 12px",
                      borderBottom: "1px solid var(--border)",
                      borderLeft: isBest ? "3px solid var(--accent)" : "1px solid var(--border)",
                      color: "var(--text)",
                    }}
                  >
                    {row.render(m)}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
