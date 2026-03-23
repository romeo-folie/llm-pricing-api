"use client"

import type { CompareFilters } from "@/hooks/useCompareState"

interface FilterControlsProps {
  value: CompareFilters
  onChange: (f: CompareFilters) => void
}

const PRIORITIES = [
  { key: "quality",  label: "Quality" },
  { key: "cost",     label: "Cost" },
  { key: "speed",    label: "Speed" },
  { key: "balanced", label: "Balanced" },
] as const

const CONTEXT_OPTIONS = [
  { value: null,    label: "Any" },
  { value: 8000,    label: "8k+" },
  { value: 32000,   label: "32k+" },
  { value: 128000,  label: "128k+" },
  { value: 200000,  label: "200k+" },
]

function sliderToPrice(v: number): number {
  return Math.exp((v / 100) * Math.log(10000)) / 100
}
function priceToSlider(p: number): number {
  return (Math.log(p * 100) / Math.log(10000)) * 100
}
function formatPrice(p: number): string {
  if (p >= 10) return `$${p.toFixed(0)}`
  if (p >= 1)  return `$${p.toFixed(2)}`
  return `$${p.toFixed(3)}`
}

export default function FilterControls({ value, onChange }: FilterControlsProps) {
  const sliderVal = value.maxPriceInput != null ? priceToSlider(value.maxPriceInput) : 100

  return (
    <div
      className="border p-4 mb-6 flex flex-col gap-4"
      style={{ borderColor: "var(--border)", backgroundColor: "var(--surface)" }}
    >
      {/* Priority toggle */}
      <div>
        <label className="block text-xs font-semibold mb-2 uppercase tracking-wide" style={{ color: "var(--muted)" }}>
          Priority
        </label>
        <div className="flex gap-2 flex-wrap" role="group" aria-label="Priority selection">
          {PRIORITIES.map((p) => (
            <button
              key={p.key}
              onClick={() => onChange({ ...value, priority: p.key })}
              className="font-orbitron px-3 py-1.5 text-xs border transition-colors"
              style={{
                borderColor: value.priority === p.key ? "var(--accent)" : "var(--border)",
                backgroundColor: value.priority === p.key ? "var(--accent)" : "var(--bg)",
                color: value.priority === p.key ? "white" : "var(--muted)",
                cursor: "pointer",
                letterSpacing: "0.05em",
                borderRadius: 0,
              }}
              aria-pressed={value.priority === p.key}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {/* Budget slider */}
      <div>
        <label
          htmlFor="budget-slider"
          className="flex items-center justify-between text-xs font-semibold uppercase tracking-wide mb-2"
          style={{ color: "var(--muted)" }}
        >
          <span>Max Budget</span>
          <span style={{ color: "var(--ink)" }}>
            {value.maxPriceInput != null
              ? `${formatPrice(value.maxPriceInput)}/1M tokens`
              : "No limit"}
          </span>
        </label>
        <input
          id="budget-slider"
          type="range"
          min={0}
          max={100}
          step={1}
          value={sliderVal}
          onChange={(e) => {
            const v = Number(e.target.value)
            onChange({
              ...value,
              maxPriceInput: v >= 100 ? null : sliderToPrice(v),
            })
          }}
          className="w-full"
          style={{ accentColor: "var(--accent)" }}
          aria-label="Maximum price per 1M input tokens"
        />
        <div className="flex justify-between text-xs mt-1" style={{ color: "var(--muted)" }}>
          <span>$0.01</span>
          <span>$100</span>
        </div>
      </div>

      {/* Context window */}
      <div>
        <label htmlFor="context-select" className="block text-xs font-semibold mb-2 uppercase tracking-wide" style={{ color: "var(--muted)" }}>
          Context Window
        </label>
        <select
          id="context-select"
          value={value.contextMin ?? ""}
          onChange={(e) => onChange({ ...value, contextMin: e.target.value ? Number(e.target.value) : null })}
          className="border px-3 py-1.5 text-sm w-full appearance-none"
          style={{
            borderColor: "var(--border)",
            backgroundColor: "var(--bg)",
            color: "var(--ink)",
            borderRadius: 0,
            paddingRight: "32px",
            backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' fill='none' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.5L6 7.5L9 4.5' stroke='currentColor' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E")`,
            backgroundRepeat: "no-repeat",
            backgroundPosition: "right 10px center",
          }}
        >
          {CONTEXT_OPTIONS.map((opt) => (
            <option key={String(opt.value)} value={opt.value ?? ""}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>

      {/* Toggles */}
      <div className="flex gap-6 flex-wrap">
        <label className="flex items-center gap-2 text-sm cursor-pointer" style={{ color: "var(--ink)" }}>
          <input
            type="checkbox"
            checked={value.requiresTools}
            onChange={(e) => onChange({ ...value, requiresTools: e.target.checked })}
            className="w-4 h-4"
            style={{ accentColor: "var(--accent)" }}
          />
          Tool calling
        </label>
        <label className="flex items-center gap-2 text-sm cursor-pointer" style={{ color: "var(--ink)" }}>
          <input
            type="checkbox"
            checked={value.requiresStructuredOutput}
            onChange={(e) => onChange({ ...value, requiresStructuredOutput: e.target.checked })}
            className="w-4 h-4"
            style={{ accentColor: "var(--accent)" }}
          />
          Structured output
        </label>
      </div>
    </div>
  )
}
