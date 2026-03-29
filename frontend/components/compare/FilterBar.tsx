"use client"

import { USE_CASES } from "@/lib/use-cases"

interface FilterBarProps {
  useCase: string | null
  onUseCaseChange: (slug: string | null) => void
  contextMin: number | null
  onContextMinChange: (v: number | null) => void
  requiresTools: boolean
  onRequiresToolsChange: (v: boolean) => void
  requiresStructuredOutput: boolean
  onRequiresStructuredOutputChange: (v: boolean) => void
}

const CONTEXT_OPTIONS: { label: string; value: number | null }[] = [
  { label: "Any", value: null },
  { label: "8K+", value: 8_000 },
  { label: "32K+", value: 32_000 },
  { label: "128K+", value: 128_000 },
  { label: "200K+", value: 200_000 },
]

export default function FilterBar({
  useCase,
  onUseCaseChange,
  contextMin,
  onContextMinChange,
  requiresTools,
  onRequiresToolsChange,
  requiresStructuredOutput,
  onRequiresStructuredOutputChange,
}: FilterBarProps) {
  return (
    <div
      className="animate-wireframe-fade"
      style={{ display: "flex", flexDirection: "column", gap: "16px", animationDelay: "1.3s" }}
    >
      {/* Use-case chips */}
      <div>
        <div
          className="font-orbitron text-xs tracking-wide mb-2"
          style={{ color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.08em" }}
        >
          Use Case
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: "8px" }}>
          {USE_CASES.map((uc) => {
            const active = useCase === uc.slug
            return (
              <button
                key={uc.slug}
                onClick={() => onUseCaseChange(active ? null : uc.slug)}
                className="font-outfit text-xs"
                style={{
                  padding: "5px 12px",
                  border: active ? "1px solid var(--accent)" : "1px solid var(--border)",
                  backgroundColor: active ? "var(--accentLt)" : "var(--bg)",
                  color: active ? "var(--accent)" : "var(--muted)",
                  cursor: "pointer",
                  transition: "all 0.15s",
                  fontWeight: active ? 600 : 400,
                }}
              >
                {uc.icon} {uc.label}
              </button>
            )
          })}
        </div>
      </div>

      {/* Secondary filters row */}
      <div style={{ display: "flex", flexWrap: "wrap", gap: "16px", alignItems: "center" }}>
        {/* Context window */}
        <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
          <label
            className="font-outfit text-xs"
            style={{ color: "var(--muted)", whiteSpace: "nowrap" }}
            htmlFor="ctx-select"
          >
            Context
          </label>
          <select
            id="ctx-select"
            value={contextMin ?? ""}
            onChange={(e) =>
              onContextMinChange(e.target.value ? Number(e.target.value) : null)
            }
            className="font-outfit text-xs"
            style={{
              padding: "4px 8px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--bg)",
              color: "var(--ink)",
              cursor: "pointer",
            }}
          >
            {CONTEXT_OPTIONS.map((opt) => (
              <option key={opt.label} value={opt.value ?? ""}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        {/* Tool calling */}
        <label
          className="font-outfit text-xs"
          style={{ display: "flex", alignItems: "center", gap: "4px", color: "var(--muted)", cursor: "pointer" }}
        >
          <input
            type="checkbox"
            checked={requiresTools}
            onChange={(e) => onRequiresToolsChange(e.target.checked)}
            style={{ accentColor: "var(--accent)" }}
          />
          Tool calling
        </label>

        {/* Structured output */}
        <label
          className="font-outfit text-xs"
          style={{ display: "flex", alignItems: "center", gap: "4px", color: "var(--muted)", cursor: "pointer" }}
        >
          <input
            type="checkbox"
            checked={requiresStructuredOutput}
            onChange={(e) => onRequiresStructuredOutputChange(e.target.checked)}
            style={{ accentColor: "var(--accent)" }}
          />
          Structured output
        </label>
      </div>
    </div>
  )
}
