"use client"

import { USE_CASES } from "@/lib/use-cases"

interface FilterBarProps {
  useCase: string | null
  onUseCaseChange: (slug: string | null) => void
  requiresTools: boolean
  onRequiresToolsChange: (v: boolean) => void
  requiresStructuredOutput: boolean
  onRequiresStructuredOutputChange: (v: boolean) => void
}

function ToggleButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className="font-orbitron text-xs"
      style={{
        padding: "5px 12px",
        border: active ? "1px solid var(--accent)" : "1px solid var(--border)",
        backgroundColor: active ? "var(--accent)" : "var(--bg)",
        color: active ? "white" : "var(--muted)",
        cursor: "pointer",
        transition: "all 0.15s",
        letterSpacing: "0.04em",
        borderRadius: 0,
        whiteSpace: "nowrap",
      }}
      aria-pressed={active}
    >
      {children}
    </button>
  )
}

export default function FilterBar({
  useCase,
  onUseCaseChange,
  requiresTools,
  onRequiresToolsChange,
  requiresStructuredOutput,
  onRequiresStructuredOutputChange,
}: FilterBarProps) {
  return (
    <div>
      {/* Single row: use-case chips left, capability toggles right */}
      <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", alignItems: "center", justifyContent: "space-between" }}>
        {/* Use-case chips — left group */}
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", alignItems: "center" }}>
          {USE_CASES.map((uc) => (
            <ToggleButton
              key={uc.slug}
              active={useCase === uc.slug}
              onClick={() => onUseCaseChange(useCase === uc.slug ? null : uc.slug)}
            >
              {uc.icon} {uc.label}
            </ToggleButton>
          ))}
        </div>

        {/* Capability toggles — right group */}
        <div style={{ display: "flex", gap: "6px", alignItems: "center" }}>
          <ToggleButton active={requiresTools} onClick={() => onRequiresToolsChange(!requiresTools)}>
            Tool calling
          </ToggleButton>
          <ToggleButton active={requiresStructuredOutput} onClick={() => onRequiresStructuredOutputChange(!requiresStructuredOutput)}>
            Structured output
          </ToggleButton>
        </div>
      </div>
    </div>
  )
}
