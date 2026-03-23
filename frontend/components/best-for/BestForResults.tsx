"use client"

export interface RecommendedModel {
  id: number
  provider: string
  name: string
  slug: string
  price_input: number
  price_output: number
  scores: Record<string, number>
  rationale: string
  warnings: string[]
  fallback: boolean
}

interface BestForResultsProps {
  useCase: string
  models: RecommendedModel[]
  loading: boolean
}

function formatPricePerM(p: number): string {
  const per1M = p * 1_000_000
  if (per1M === 0) return "—"
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
              style={{ width: `${value}%`, backgroundColor: "var(--ink)", opacity: 0.7 }}
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
      <div className="h-4 w-24 mb-3 rounded" style={{ backgroundColor: "var(--border)" }} />
      <div className="h-6 w-48 mb-2 rounded" style={{ backgroundColor: "var(--border)" }} />
      <div className="h-3 w-full mb-1 rounded" style={{ backgroundColor: "var(--surfaceLo)" }} />
      <div className="h-3 w-3/4 rounded" style={{ backgroundColor: "var(--surfaceLo)" }} />
    </div>
  )
}

function TopPickCard({ model }: { model: RecommendedModel }) {
  const scoreValues = Object.values(model.scores)
  const composite = model.fallback || scoreValues.length === 0
    ? null
    : scoreValues.reduce((a, b) => a + b, 0) / scoreValues.length

  return (
    <div
      className="border-2 p-6 mb-6"
      style={{ borderColor: "var(--ink)", backgroundColor: "var(--surface)" }}
    >
      <div className="flex items-start justify-between mb-3 flex-wrap gap-2">
        <span
          className="text-xs font-semibold px-2 py-0.5 border"
          style={{ borderColor: "var(--ink)", color: "var(--ink)" }}
        >
          ⭐ Top Pick
        </span>
        <span className="text-xs" style={{ color: "var(--muted)" }}>
          {formatPricePerM(model.price_input)} input
        </span>
      </div>

      <div className="mb-1">
        <span className="text-xs uppercase tracking-wide mr-2" style={{ color: "var(--muted)" }}>
          {model.provider}
        </span>
        <h2 className="font-orbitron text-lg font-bold inline" style={{ color: "var(--ink)" }}>
          {model.name}
        </h2>
      </div>

      {composite !== null ? (
        <div className="text-2xl font-mono font-bold mb-3" style={{ color: "var(--ink)" }}>
          {composite.toFixed(1)}
          <span className="text-sm font-normal ml-1" style={{ color: "var(--muted)" }}>/ 100</span>
        </div>
      ) : (
        <div className="text-sm italic mb-3" style={{ color: "var(--muted)" }}>No capability data — ranked by price</div>
      )}

      {model.rationale && (
        <p className="text-sm mb-4" style={{ color: "var(--ink)" }}>
          {model.rationale}
        </p>
      )}

      {/* Score breakdown */}
      {Object.keys(model.scores).length > 0 && (
        <div className="mb-4">
          <div className="text-xs uppercase tracking-wide mb-2 font-semibold" style={{ color: "var(--muted)" }}>
            Score Breakdown
          </div>
          {Object.entries(model.scores).map(([dim, val]) => (
            <ScoreBar key={dim} label={dim} value={val} />
          ))}
        </div>
      )}

      {/* Warnings */}
      {model.warnings && model.warnings.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {model.warnings.map((w, i) => (
            <span
              key={i}
              className="text-xs px-2 py-0.5 border"
              style={{ borderColor: "#B45309", color: "#B45309", backgroundColor: "#FEF3C7" }}
            >
              ⚠ {w}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function AlternativeCard({ model }: { model: RecommendedModel }) {
  const scoreValues = Object.values(model.scores)
  const topDim = scoreValues.length > 0
    ? Object.entries(model.scores).sort((a, b) => b[1] - a[1])[0]
    : null

  return (
    <div
      className="border p-4 flex flex-col gap-1"
      style={{ borderColor: "var(--border)", backgroundColor: "var(--surface)" }}
    >
      <div className="text-xs" style={{ color: "var(--muted)" }}>{model.provider}</div>
      <div className="font-semibold text-sm" style={{ color: "var(--ink)" }}>{model.name}</div>
      {topDim ? (
        <div className="text-xs" style={{ color: "var(--muted)" }}>
          {topDim[0].replace(/_/g, " ")}: <span style={{ color: "var(--ink)" }}>{topDim[1].toFixed(1)}</span>
        </div>
      ) : (
        <div className="text-xs italic" style={{ color: "var(--muted)" }}>No capability data</div>
      )}
      <div className="text-xs mt-1" style={{ color: "var(--muted)" }}>
        {formatPricePerM(model.price_input)} input
      </div>
    </div>
  )
}

export default function BestForResults({ useCase, models, loading }: BestForResultsProps) {
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
        <div className="text-2xl mb-2">🔍</div>
        <div className="font-semibold mb-1" style={{ color: "var(--ink)" }}>No models matched</div>
        <div className="text-sm">Try adjusting the filters — lower the budget or remove constraints.</div>
      </div>
    )
  }

  const [topPick, ...alternatives] = models

  return (
    <div>
      <TopPickCard model={topPick} />
      {alternatives.length > 0 && (
        <div>
          <div className="text-xs uppercase tracking-wide mb-3 font-semibold" style={{ color: "var(--muted)" }}>
            Alternatives
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            {alternatives.slice(0, 4).map((m) => (
              <AlternativeCard key={m.id} model={m} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
