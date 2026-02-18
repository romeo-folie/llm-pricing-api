"use client"

import { useState, useEffect, useCallback, useId } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
import { calculateCost, type CostResult } from "@/app/calculator/actions"
import ModelPicker from "@/components/compare/ModelPicker"

interface CalculatorClientProps {
  allModels: Model[]
}

type Period = "daily" | "monthly" | "yearly"
const PERIODS: { label: string; value: Period }[] = [
  { label: "Daily",   value: "daily"   },
  { label: "Monthly", value: "monthly" },
  { label: "Yearly",  value: "yearly"  },
]

function formatCurrency(n: number): string {
  return n.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 })
}

const MAX_TOKENS = 10_000_000_000

export default function CalculatorClient({ allModels }: CalculatorClientProps) {
  const router       = useRouter()
  const searchParams = useSearchParams()

  const [selectedIds,    setSelectedIds]    = useState<string[]>(() => (searchParams.get("models") ?? "").split(",").filter(Boolean))
  const [inputTokens,    setInputTokens]    = useState<number>(() => Number(searchParams.get("input")  ?? 100_000))
  const [outputTokens,   setOutputTokens]   = useState<number>(() => Number(searchParams.get("output") ?? 10_000))
  const [period,         setPeriod]         = useState<Period>((searchParams.get("period") as Period) ?? "monthly")
  const [results,        setResults]        = useState<CostResult[]>([])
  const [calcError,      setCalcError]      = useState<string | null>(null)

  const updateUrl = useCallback((ids: string[], inp: number, out: number, per: Period) => {
    const params = new URLSearchParams()
    if (ids.length)  params.set("models", ids.join(","))
    params.set("input",  String(inp))
    params.set("output", String(out))
    params.set("period", per)
    router.replace(`?${params.toString()}`, { scroll: false })
  }, [router])

  const runCalc = useCallback(async (ids: string[], inp: number, out: number, per: Period) => {
    if (ids.length === 0) { setResults([]); setCalcError(null); return }
    try {
      const r = await calculateCost(ids, inp, out, per)
      setResults(r)
      setCalcError(null)
    } catch (e) {
      setCalcError(e instanceof Error ? e.message : "Calculation failed")
    }
  }, [])

  useEffect(() => {
    void runCalc(selectedIds, inputTokens, outputTokens, period)
    updateUrl(selectedIds, inputTokens, outputTokens, period)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIds, inputTokens, outputTokens, period])

  function handleSelect(id: string) {
    if (selectedIds.includes(id)) return
    const next = selectedIds.length >= 3 ? [...selectedIds.slice(1), id] : [...selectedIds, id]
    setSelectedIds(next)
  }

  function handleRemove(id: string) {
    setSelectedIds(selectedIds.filter((i) => i !== id))
  }

  const emptyGridId = useId()

  const cheapestId = results.length > 0
    ? results.reduce((a, b) => a.total < b.total ? a : b).modelId
    : null

  return (
    <div>
      <div style={{ marginBottom: "24px" }}>
        <h1 className="font-orbitron text-2xl font-bold" style={{ color: "var(--ink)" }}>
          Cost Calculator
        </h1>
        <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
          Estimate your LLM API costs by token volume
        </p>
      </div>

      {/* HUD / control panel */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: "16px",
          padding: "20px",
          border: "1px solid var(--borderDk)",
          borderTop: "3px solid var(--accent)",
          backgroundColor: "var(--surfaceLo)",
          marginBottom: "24px",
        }}
      >
        {/* Model picker — spans full width */}
        <div style={{ gridColumn: "1 / -1" }}>
          <label className="font-orbitron text-xs" style={{ color: "var(--dim)", display: "block", marginBottom: "6px", textTransform: "uppercase", letterSpacing: "0.08em" }}>
            Models (up to 3)
          </label>
          <ModelPicker
            models={allModels}
            selected={selectedIds}
            onSelect={handleSelect}
            onRemove={handleRemove}
            max={3}
            placeholder="Search and add models…"
          />
        </div>

        {/* Input tokens */}
        <div>
          <label className="font-orbitron text-xs" style={{ color: "var(--dim)", display: "block", marginBottom: "6px", textTransform: "uppercase", letterSpacing: "0.08em" }}>
            Input tokens / day
          </label>
          <input
            type="number"
            min={0}
            max={MAX_TOKENS}
            value={inputTokens}
            onChange={(e) => setInputTokens(Math.min(MAX_TOKENS, Math.max(0, Number(e.target.value))))}
            className="font-orbitron text-sm w-full"
            style={{
              padding: "8px 12px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: "var(--surface)",
              color: "var(--text)",
              outline: "none",
              width: "100%",
            }}
          />
        </div>

        {/* Output tokens */}
        <div>
          <label className="font-orbitron text-xs" style={{ color: "var(--dim)", display: "block", marginBottom: "6px", textTransform: "uppercase", letterSpacing: "0.08em" }}>
            Output tokens / day
          </label>
          <input
            type="number"
            min={0}
            max={MAX_TOKENS}
            value={outputTokens}
            onChange={(e) => setOutputTokens(Math.min(MAX_TOKENS, Math.max(0, Number(e.target.value))))}
            className="font-orbitron text-sm w-full"
            style={{
              padding: "8px 12px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: "var(--surface)",
              color: "var(--text)",
              outline: "none",
              width: "100%",
            }}
          />
        </div>

        {/* Period toggle */}
        <div style={{ gridColumn: "1 / -1" }}>
          <label className="font-orbitron text-xs" style={{ color: "var(--dim)", display: "block", marginBottom: "6px", textTransform: "uppercase", letterSpacing: "0.08em" }}>
            View as
          </label>
          <div style={{ display: "flex", gap: "4px" }}>
            {PERIODS.map((p) => (
              <button
                key={p.value}
                onClick={() => setPeriod(p.value)}
                className="font-orbitron text-xs"
                style={{
                  padding: "6px 14px",
                  border: "1px solid var(--border)",
                  borderRadius: "2px",
                  backgroundColor: period === p.value ? "var(--accent)" : "var(--surface)",
                  color: period === p.value ? "white" : "var(--muted)",
                  cursor: "pointer",
                }}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Calculation error */}
      {calcError && (
        <div
          className="font-outfit text-sm"
          style={{
            padding: "12px 16px",
            border: "1px solid var(--red)",
            borderRadius: "2px",
            color: "var(--red)",
            backgroundColor: "var(--redLt)",
            marginBottom: "16px",
          }}
        >
          {calcError}
        </div>
      )}

      {/* Results */}
      {results.length === 0 ? (
        <div
          className="font-outfit text-sm"
          style={{
            position: "relative",
            textAlign: "center",
            padding: "64px 24px",
            border: "1px solid var(--border)",
            color: "var(--muted)",
            overflow: "hidden",
          }}
        >
          <svg aria-hidden="true" style={{ position: "absolute", inset: 0, width: "100%", height: "100%", opacity: 0.06, pointerEvents: "none" }}>
            <defs>
              <pattern id={emptyGridId} x="0" y="0" width="40" height="40" patternUnits="userSpaceOnUse" patternTransform="rotate(-30) skewX(20)">
                <path d="M 40 0 L 0 0 0 40" fill="none" stroke="var(--borderDk)" strokeWidth="0.5" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill={`url(#${emptyGridId})`} />
          </svg>
          <span style={{ position: "relative" }}>Add a model above to see cost estimates.</span>
        </div>
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: "12px" }}>
          {results.map((r) => {
            const isCheapest = r.modelId === cheapestId
            return (
              <div
                key={r.modelId}
                style={{
                  padding: "16px",
                  border: "1px solid var(--border)",
                  borderTop: isCheapest ? "3px solid var(--accent)" : "3px solid var(--border)",
                  backgroundColor: isCheapest ? "var(--accentLt)" : "var(--surfaceHi)",
                }}
              >
                <div className="font-outfit text-sm" style={{ color: "var(--muted)", marginBottom: "4px" }}>
                  {r.modelName}
                </div>
                {isCheapest && (
                  <div className="font-orbitron text-xs" style={{ color: "var(--accent)", marginBottom: "8px", letterSpacing: "0.06em" }}>
                    ★ CHEAPEST
                  </div>
                )}
                <div className="font-orbitron text-2xl" style={{ color: "var(--ink)", marginBottom: "12px" }}>
                  {formatCurrency(r.total)}
                </div>
                <div style={{ borderTop: "1px solid var(--border)", paddingTop: "8px", display: "flex", flexDirection: "column", gap: "4px" }}>
                  <div style={{ display: "flex", justifyContent: "space-between" }}>
                    <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>Input</span>
                    <span className="font-orbitron text-xs" style={{ color: "var(--text)" }}>{formatCurrency(r.inputCost)}</span>
                  </div>
                  <div style={{ display: "flex", justifyContent: "space-between" }}>
                    <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>Output</span>
                    <span className="font-orbitron text-xs" style={{ color: "var(--text)" }}>{formatCurrency(r.outputCost)}</span>
                  </div>
                  <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginTop: "4px", textAlign: "center" }}>
                    per {period === "daily" ? "day" : period === "monthly" ? "month" : "year"}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
