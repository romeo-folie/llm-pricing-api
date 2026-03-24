"use client"

export interface RecommendedModel {
  id: number
  provider: string
  name: string
  slug: string
  price_input: number
  price_output: number
  scores: Record<string, number> | null
  rationale: string
  fallback: boolean
}

interface ResultsPanelProps {
  useCase: string
  models: RecommendedModel[]
  loading: boolean
  warnings?: string[]
  selectedModels: string[]
  onSelect: (slug: string) => void
}

/**
 * Consolidates related warning strings into a single line.
 *
 * Warnings that share a common prefix (e.g. "Limited benchmark coverage for X",
 * "Limited benchmark coverage for Y") are grouped under that prefix with the
 * subjects joined by ", ". Unrelated warnings are appended after a space.
 *
 * Example input:  ["Limited benchmark coverage for agentic",
 *                  "Limited benchmark coverage for tool_use",
 *                  "Limited benchmark coverage for reasoning"]
 * Example output: "Limited benchmark coverage for: agentic, tool_use, reasoning"
 */
function consolidateWarnings(warnings: string[]): string {
  const groups: Record<string, string[]> = {}
  const ungrouped: string[] = []

  // Pattern: "<prefix> for <subject>" — group by prefix.
  const forPattern = /^(.+) for (.+)$/
  for (const w of warnings) {
    const m = w.match(forPattern)
    if (m) {
      const prefix = m[1]
      if (!groups[prefix]) groups[prefix] = []
      groups[prefix].push(m[2].replace(/_/g, " "))
    } else {
      ungrouped.push(w)
    }
  }

  const parts: string[] = Object.entries(groups).map(
    ([prefix, subjects]) =>
      subjects.length === 1
        ? `${prefix} for ${subjects[0]}`
        : `${prefix} for: ${subjects.join(", ")}`
  )

  return [...parts, ...ungrouped].join(" · ")
}

function formatPricePerM(p: number): string {
  const per1M = p * 1_000_000
  if (per1M === 0) return "\u2014"
  if (per1M >= 10) return `$${per1M.toFixed(0)}/1M`
  if (per1M >= 1)  return `$${per1M.toFixed(2)}/1M`
  return `$${per1M.toFixed(3)}/1M`
}

function ScoreBar({ label, value }: { label: string; value: number | undefined }) {
  const missing = value === undefined || value === null
  return (
    <div className="flex items-center gap-2 text-xs mb-1">
      <span className="w-28 shrink-0 capitalize" style={{ color: "var(--muted)" }}>
        {label.replace(/_/g, " ")}
      </span>
      {missing ? (
        <span className="text-xs italic" style={{ color: "var(--muted)" }}>No data</span>
      ) : (
        <>
          <div className="flex-1 h-2 border overflow-hidden" style={{ borderColor: "var(--border)", backgroundColor: "var(--surfaceLo)" }}>
            <div
              className="h-full transition-all duration-500"
              style={{ width: `${value}%`, backgroundColor: "var(--accent)" }}
            />
          </div>
          <span className="w-10 text-right tabular-nums" style={{ color: "var(--ink)" }}>
            {value.toFixed(1)}
          </span>
        </>
      )}
    </div>
  )
}

function SkeletonCard({ compact = false }: { compact?: boolean }) {
  return (
    <div
      className={`border p-5 animate-pulse ${compact ? "" : "mb-6"}`}
      style={{ borderColor: "var(--border)", backgroundColor: "var(--surface)" }}
    >
      <div className="h-4 w-24 mb-3" style={{ backgroundColor: "var(--border)" }} />
      <div className="h-6 w-48 mb-2" style={{ backgroundColor: "var(--border)" }} />
      <div className="h-3 w-full mb-1" style={{ backgroundColor: "var(--surfaceLo)" }} />
      <div className="h-3 w-3/4" style={{ backgroundColor: "var(--surfaceLo)" }} />
    </div>
  )
}

function SelectionCheckbox({
  checked,
  slug,
  name,
  parentHovered,
  onClick,
}: {
  checked: boolean
  slug: string
  name: string
  parentHovered: boolean
  onClick: (e: React.MouseEvent) => void
}) {
  return (
    <button
      role="checkbox"
      aria-checked={checked}
      aria-label={`Select ${name} for comparison`}
      onClick={onClick}
      className="w-5 h-5 border flex items-center justify-center transition-opacity"
      style={{
        opacity: checked || parentHovered ? 1 : 0,
        borderColor: checked ? "var(--accent)" : "var(--border)",
        backgroundColor: checked ? "var(--accent)" : "transparent",
        cursor: "pointer",
        flexShrink: 0,
        borderRadius: 0,
      }}
    >
      {checked && (
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
          <path d="M2.5 6L5 8.5L9.5 3.5" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )}
    </button>
  )
}

function WarningBadge({ text }: { text: string }) {
  return (
    <span
      className="text-xs px-2 py-0.5 border"
      style={{
        borderColor: "var(--warningBorder)",
        color: "var(--warningText)",
        backgroundColor: "var(--warningBg)",
      }}
    >
      {text}
    </span>
  )
}

function TopPickCard({
  model,
  isSelected,
  onSelect,
}: {
  model: RecommendedModel
  isSelected: boolean
  onSelect: (slug: string) => void
}) {
  const scoreValues = Object.values(model.scores ?? {})
  const composite = model.fallback || scoreValues.length === 0
    ? null
    : scoreValues.reduce((a, b) => a + b, 0) / scoreValues.length

  return (
    <div
      className="border p-6 mb-6 group relative"
      style={{
        borderColor: isSelected ? "var(--accent)" : "var(--border)",
        borderLeftWidth: isSelected ? "3px" : "1px",
        backgroundColor: "var(--surface)",
      }}
    >
      <div className="absolute top-4 right-4 opacity-0 group-hover:opacity-100 transition-opacity"
        style={{ opacity: isSelected ? 1 : undefined }}
      >
        <SelectionCheckbox
          checked={isSelected}
          slug={model.slug}
          name={model.name}
          parentHovered={false}
          onClick={(e) => { e.stopPropagation(); onSelect(model.slug) }}
        />
      </div>

      <div className="flex items-start justify-between mb-3 flex-wrap gap-2 pr-8">
        <span
          className="font-orbitron text-xs font-semibold px-2 py-0.5 border"
          style={{ borderColor: "var(--accent)", color: "var(--accent)", letterSpacing: "0.06em" }}
        >
          Top Pick
        </span>
        <span className="font-orbitron text-xs" style={{ color: "var(--muted)" }}>
          {formatPricePerM(model.price_input)} input
        </span>
      </div>

      <div className="mb-1">
        <span className="font-outfit text-xs uppercase tracking-wide mr-2" style={{ color: "var(--muted)" }}>
          {model.provider}
        </span>
        <h2 className="font-outfit text-lg font-bold inline" style={{ color: "var(--ink)" }}>
          {model.name}
        </h2>
      </div>

      {composite !== null ? (
        <div className="font-orbitron text-2xl font-bold mb-3" style={{ color: "var(--ink)" }}>
          {composite.toFixed(1)}
          <span className="text-sm font-normal ml-1" style={{ color: "var(--muted)" }}>/ 100</span>
        </div>
      ) : (
        <div className="text-sm italic mb-3" style={{ color: "var(--muted)" }}>No capability data — ranked by price</div>
      )}

      {model.rationale && (
        <p className="font-outfit text-sm mb-4" style={{ color: "var(--ink)" }}>
          {model.rationale}
        </p>
      )}

      {Object.keys(model.scores ?? {}).length > 0 && (
        <div className="mb-4">
          <div className="text-xs uppercase tracking-wide mb-2 font-semibold" style={{ color: "var(--muted)" }}>
            Score Breakdown
          </div>
          {Object.entries(model.scores ?? {}).map(([dim, val]) => (
            <ScoreBar key={dim} label={dim} value={val} />
          ))}
        </div>
      )}


    </div>
  )
}

function AlternativeCard({
  model,
  isSelected,
  onSelect,
}: {
  model: RecommendedModel
  isSelected: boolean
  onSelect: (slug: string) => void
}) {
  const topDim = Object.values(model.scores ?? {}).length > 0
    ? Object.entries(model.scores ?? {}).sort((a, b) => b[1] - a[1])[0]
    : null

  return (
    <div
      className="border p-4 flex flex-col gap-1 group relative cursor-pointer transition-colors"
      style={{
        borderColor: isSelected ? "var(--accent)" : "var(--border)",
        borderLeftWidth: isSelected ? "3px" : "1px",
        backgroundColor: "var(--surface)",
      }}
      onClick={() => onSelect(model.slug)}
      onMouseEnter={(e) => {
        if (!isSelected) e.currentTarget.style.backgroundColor = "var(--surfaceHi)"
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor = "var(--surface)"
      }}
    >
      <div className="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity"
        style={{ opacity: isSelected ? 1 : undefined }}
      >
        <SelectionCheckbox
          checked={isSelected}
          slug={model.slug}
          name={model.name}
          parentHovered={false}
          onClick={(e) => { e.stopPropagation(); onSelect(model.slug) }}
        />
      </div>

      <div className="font-outfit text-xs pr-6" style={{ color: "var(--muted)" }}>{model.provider}</div>
      <div className="font-outfit font-semibold text-sm" style={{ color: "var(--ink)" }}>{model.name}</div>
      {topDim ? (
        <div className="text-xs" style={{ color: "var(--muted)" }}>
          {topDim[0].replace(/_/g, " ")}: <span style={{ color: "var(--ink)" }}>{topDim[1].toFixed(1)}</span>
        </div>
      ) : (
        <div className="text-xs italic" style={{ color: "var(--muted)" }}>No capability data</div>
      )}
      <div className="font-orbitron text-xs mt-1" style={{ color: "var(--muted)" }}>
        {formatPricePerM(model.price_input)} input
      </div>
    </div>
  )
}

export default function ResultsPanel({
  models,
  loading,
  warnings = [],
  selectedModels,
  onSelect,
}: ResultsPanelProps) {
  if (loading) {
    return (
      <div>
        <SkeletonCard />
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          {[0, 1, 2, 3].map((i) => <SkeletonCard key={i} compact />)}
        </div>
      </div>
    )
  }

  if (models.length === 0) {
    return (
      <div
        className="border p-8 text-center"
        style={{ borderColor: "var(--border)", color: "var(--muted)" }}
      >
        <div className="text-2xl mb-2">&#x1F50D;</div>
        <div className="font-semibold mb-1" style={{ color: "var(--ink)" }}>No models matched</div>
        <div className="text-sm">Try adjusting the filters — lower the budget or remove constraints.</div>
      </div>
    )
  }

  const [topPick, ...alternatives] = models

  return (
    /* pb-20 compensates for the sticky CTA bar height on mobile so content isn't hidden beneath it */
    <div className={selectedModels.length >= 1 ? "pb-20" : undefined}>
      {warnings.length > 0 && (
        <div className="mb-4">
          <div
            className="text-xs px-3 py-2 border"
            style={{
              borderColor: "var(--warningBorder)",
              color: "var(--warningText)",
              backgroundColor: "var(--warningBg)",
            }}
          >
            {consolidateWarnings(warnings)}
          </div>
        </div>
      )}

      <TopPickCard
        model={topPick}
        isSelected={selectedModels.includes(topPick.slug)}
        onSelect={onSelect}
      />

      {alternatives.length > 0 && (
        <div>
          <div className="text-xs uppercase tracking-wide mb-3 font-semibold" style={{ color: "var(--muted)" }}>
            Alternatives
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            {alternatives.slice(0, 4).map((m) => (
              <AlternativeCard
                key={m.id}
                model={m}
                isSelected={selectedModels.includes(m.slug)}
                onSelect={onSelect}
              />
            ))}
          </div>
        </div>
      )}

      {/* Sticky CTA bar */}
      {selectedModels.length >= 1 && (
        <div
          className="fixed bottom-0 left-0 right-0 z-40 border-t py-3 px-4"
          style={{
            backgroundColor: "var(--surface)",
            borderColor: "var(--border)",
            boxShadow: "0 -2px 8px rgba(0, 0, 0, 0.08)",
          }}
        >
          <div className="mx-auto max-w-7xl flex items-center justify-between">
            <span className="font-outfit text-sm" style={{ color: "var(--ink)" }}>
              {selectedModels.length} of 2 selected
            </span>
            <button
              disabled={selectedModels.length < 2}
              className="px-4 py-2 text-sm font-semibold transition-opacity"
              style={{
                backgroundColor: selectedModels.length >= 2 ? "var(--accent)" : "var(--border)",
                color: selectedModels.length >= 2 ? "white" : "var(--muted)",
                cursor: selectedModels.length >= 2 ? "pointer" : "not-allowed",
                opacity: selectedModels.length >= 2 ? 1 : 0.6,
                borderRadius: 0,
              }}
              onClick={() => {
                const panel = document.getElementById("compare-panel")
                if (panel) panel.scrollIntoView({ behavior: "smooth" })
              }}
            >
              Compare &rarr;
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

export { ScoreBar }
