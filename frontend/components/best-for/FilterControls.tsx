"use client"

export interface FilterState {
  priority: "quality" | "cost" | "speed" | "balanced"
  maxPriceInput: number | null // $/1M tokens
  contextMin: number | null   // tokens
  requiresTools: boolean
  requiresStructuredOutput: boolean
}

interface FilterControlsProps {
  value: FilterState
  onChange: (f: FilterState) => void
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

// Log-scale slider: maps 0–100 range to $0.01–$100/1M
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
              className="px-3 py-1.5 text-sm border transition-colors"
              style={
                value.priority === p.key
                  ? { borderColor: "var(--ink)", backgroundColor: "var(--ink)", color: "var(--bg)" }
                  : { borderColor: "var(--border)", color: "var(--muted)" }
              }
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
          className="w-full accent-current"
          style={{ accentColor: "var(--ink)" }}
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
          className="border px-3 py-1.5 text-sm w-full"
          style={{ borderColor: "var(--border)", backgroundColor: "var(--surface)", color: "var(--ink)" }}
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
            style={{ accentColor: "var(--ink)" }}
          />
          Tool calling
        </label>
        <label className="flex items-center gap-2 text-sm cursor-pointer" style={{ color: "var(--ink)" }}>
          <input
            type="checkbox"
            checked={value.requiresStructuredOutput}
            onChange={(e) => onChange({ ...value, requiresStructuredOutput: e.target.checked })}
            className="w-4 h-4"
            style={{ accentColor: "var(--ink)" }}
          />
          Structured output
        </label>
      </div>
    </div>
  )
}
