"use client"

import { useState, useRef, useEffect } from "react"
import { ScoreBar } from "./ResultsPanel"
import type { RecommendedModel } from "./ResultsPanel"

export interface CompareModel {
  id: number
  provider: string
  name: string
  slug: string
  price_input: number
  price_output: number
  context_window: number | null
  scores: Record<string, number>
  freshness: Record<string, string>
  rationale: string
  warnings: string[]
}

interface ComparePanelProps {
  modelA: CompareModel
  modelB: CompareModel
  useCase: string | null
  availableModels: RecommendedModel[]
  onSwap: (position: "a" | "b", newSlug: string) => void
  onClear: () => void
  loading?: boolean
}

function formatPricePerM(p: number): string {
  const per1M = p * 1_000_000
  if (per1M === 0) return "\u2014"
  if (per1M >= 10) return `$${per1M.toFixed(0)}/1M`
  if (per1M >= 1)  return `$${per1M.toFixed(2)}/1M`
  return `$${per1M.toFixed(3)}/1M`
}

function formatContext(ctx: number | null): string {
  if (!ctx) return "\u2014"
  if (ctx >= 1000) return `${(ctx / 1000).toFixed(0)}k`
  return String(ctx)
}

function SwapPicker({
  availableModels,
  currentSlug,
  onSelect,
  onClose,
}: {
  availableModels: RecommendedModel[]
  currentSlug: string
  onSelect: (slug: string) => void
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    document.addEventListener("mousedown", handleClick)
    return () => document.removeEventListener("mousedown", handleClick)
  }, [onClose])

  return (
    <div
      ref={ref}
      className="absolute z-50 border shadow-lg max-h-48 overflow-y-auto w-56"
      style={{ backgroundColor: "var(--surface)", borderColor: "var(--border)" }}
    >
      {availableModels
        .filter((m) => m.slug !== currentSlug)
        .map((m) => (
          <button
            key={m.slug}
            className="block w-full text-left px-3 py-2 text-sm hover:bg-gray-100 transition-colors"
            style={{ color: "var(--ink)" }}
            onClick={() => { onSelect(m.slug); onClose() }}
          >
            <span className="text-xs" style={{ color: "var(--muted)" }}>{m.provider}</span>
            <br />
            {m.name}
          </button>
        ))}
    </div>
  )
}

function ModelColumn({
  model,
  position,
  availableModels,
  onSwap,
}: {
  model: CompareModel
  position: "a" | "b"
  availableModels: RecommendedModel[]
  onSwap: (position: "a" | "b", newSlug: string) => void
}) {
  const [showSwap, setShowSwap] = useState(false)
  const scoreValues = Object.values(model.scores)
  const composite = scoreValues.length > 0
    ? scoreValues.reduce((a, b) => a + b, 0) / scoreValues.length
    : null

  return (
    <div className="flex-1 min-w-0">
      {/* Model header */}
      <div className="mb-4">
        <div className="text-xs uppercase tracking-wide mb-1" style={{ color: "var(--muted)" }}>
          {model.provider}
        </div>
        <div className="font-orbitron text-base font-bold mb-1 truncate" style={{ color: "var(--ink)" }}>
          {model.name}
        </div>
        {composite !== null && (
          <div className="font-orbitron text-xl font-bold" style={{ color: "var(--ink)" }}>
            {composite.toFixed(1)}
            <span className="text-xs font-normal ml-1" style={{ color: "var(--muted)" }}>/100</span>
          </div>
        )}
      </div>

      {/* Pricing rows */}
      <div className="space-y-2 mb-4">
        <div>
          <div className="text-xs uppercase tracking-wide" style={{ color: "var(--muted)" }}>Input</div>
          <div className="text-sm font-semibold tabular-nums" style={{ color: "var(--ink)" }}>
            {formatPricePerM(model.price_input)}
          </div>
        </div>
        <div>
          <div className="text-xs uppercase tracking-wide" style={{ color: "var(--muted)" }}>Output</div>
          <div className="text-sm font-semibold tabular-nums" style={{ color: "var(--ink)" }}>
            {formatPricePerM(model.price_output)}
          </div>
        </div>
        <div>
          <div className="text-xs uppercase tracking-wide" style={{ color: "var(--muted)" }}>Context</div>
          <div className="text-sm font-semibold" style={{ color: "var(--ink)" }}>
            {formatContext(model.context_window)}
          </div>
        </div>
      </div>

      {/* Score bars */}
      {Object.keys(model.scores).length > 0 && (
        <div className="mb-4">
          {Object.entries(model.scores).map(([dim, val]) => (
            <ScoreBar key={dim} label={dim} value={val} />
          ))}
        </div>
      )}

      {/* Warnings */}
      {model.warnings && model.warnings.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-4">
          {model.warnings.map((w, i) => (
            <span
              key={i}
              className="text-xs px-2 py-0.5 border"
              style={{ borderColor: "#B45309", color: "#B45309", backgroundColor: "#FEF3C7" }}
            >
              {w}
            </span>
          ))}
        </div>
      )}

      {/* Swap button */}
      <div className="relative">
        <button
          className="text-xs border px-2 py-1 transition-colors"
          style={{ borderColor: "var(--border)", color: "var(--muted)", cursor: "pointer" }}
          onClick={() => setShowSwap(!showSwap)}
        >
          Swap model &#x21C5;
        </button>
        {showSwap && (
          <SwapPicker
            availableModels={availableModels}
            currentSlug={model.slug}
            onSelect={(slug) => onSwap(position, slug)}
            onClose={() => setShowSwap(false)}
          />
        )}
      </div>
    </div>
  )
}

function CompareSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-6 animate-pulse">
      {[0, 1].map((i) => (
        <div key={i}>
          <div className="h-4 w-20 mb-2 rounded" style={{ backgroundColor: "var(--border)" }} />
          <div className="h-6 w-40 mb-2 rounded" style={{ backgroundColor: "var(--border)" }} />
          <div className="h-8 w-16 mb-4 rounded" style={{ backgroundColor: "var(--border)" }} />
          <div className="space-y-3">
            {[0, 1, 2].map((j) => (
              <div key={j}>
                <div className="h-3 w-12 mb-1 rounded" style={{ backgroundColor: "var(--surfaceLo)" }} />
                <div className="h-4 w-24 rounded" style={{ backgroundColor: "var(--border)" }} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

export default function ComparePanel({
  modelA,
  modelB,
  useCase,
  availableModels,
  onSwap,
  onClear,
  loading = false,
}: ComparePanelProps) {
  const [copied, setCopied] = useState(false)

  function handleShare() {
    navigator.clipboard.writeText(window.location.href).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }

  return (
    <div
      id="compare-panel"
      className="border-t-2 mt-8 pt-6 animate-wireframe-fade"
      style={{
        borderColor: "var(--accent)",
        backgroundColor: "var(--surface)",
        padding: "24px",
        marginLeft: "-16px",
        marginRight: "-16px",
      }}
    >
      {/* Panel header */}
      <div className="flex items-center justify-between mb-6 flex-wrap gap-2">
        <h3 className="font-orbitron text-sm font-semibold tracking-wide" style={{ color: "var(--ink)" }}>
          COMPARING
        </h3>
        <div className="flex items-center gap-3">
          <button
            onClick={handleShare}
            className="text-xs border px-2 py-1 transition-colors"
            style={{
              borderColor: "var(--border)",
              color: copied ? "var(--green)" : "var(--muted)",
              cursor: "pointer",
            }}
          >
            {copied ? "Copied!" : "Share link"}
          </button>
          <button
            onClick={onClear}
            className="text-xs px-2 py-1 transition-colors"
            style={{ color: "var(--muted)", cursor: "pointer" }}
            aria-label="Clear comparison"
          >
            Clear &#x2715;
          </button>
        </div>
      </div>

      {loading ? (
        <CompareSkeleton />
      ) : (
        <>
          {/* Desktop: side by side / Mobile: stacked */}
          <div className="flex flex-col md:flex-row gap-6">
            <ModelColumn
              model={modelA}
              position="a"
              availableModels={availableModels}
              onSwap={onSwap}
            />
            <div
              className="hidden md:block w-px self-stretch"
              style={{ backgroundColor: "var(--border)" }}
            />
            <hr className="md:hidden" style={{ borderColor: "var(--border)" }} />
            <ModelColumn
              model={modelB}
              position="b"
              availableModels={availableModels}
              onSwap={onSwap}
            />
          </div>

          {/* Rationale section */}
          {(modelA.rationale || modelB.rationale) && useCase && (
            <div className="mt-6 pt-4 border-t" style={{ borderColor: "var(--border)" }}>
              <div className="text-xs uppercase tracking-wide mb-2 font-semibold" style={{ color: "var(--muted)" }}>
                Analysis
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {modelA.rationale && (
                  <p className="text-sm" style={{ color: "var(--ink)" }}>{modelA.rationale}</p>
                )}
                {modelB.rationale && (
                  <p className="text-sm" style={{ color: "var(--ink)" }}>{modelB.rationale}</p>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
