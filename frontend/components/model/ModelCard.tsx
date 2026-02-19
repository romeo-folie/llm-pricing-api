"use client"

import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"

interface ModelCardProps {
  model: Model
}

function formatPrice(p: number): string {
  return `$${p.toFixed(4)}`
}

function formatContext(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M`
  if (n >= 1_000)     return `${(n / 1_000).toFixed(0)}K`
  return String(n)
}

function ConfidenceDot({ confidence }: { confidence: "high" | "medium" | "low" }) {
  const color =
    confidence === "high"   ? "var(--green)"  :
    confidence === "medium" ? "var(--accent)" : "var(--red)"
  const label =
    confidence === "high"   ? "High confidence"   :
    confidence === "medium" ? "Medium confidence" : "Low confidence"
  return (
    <span
      aria-label={label}
      title={label}
      style={{
        display: "inline-block",
        width: "8px",
        height: "8px",
        borderRadius: "50%",
        backgroundColor: color,
        flexShrink: 0,
      }}
    />
  )
}

export default function ModelCard({ model }: ModelCardProps) {
  const router      = useRouter()
  const searchParams = useSearchParams()

  function openModal() {
    const params = new URLSearchParams(searchParams.toString())
    params.set("model", model.id)
    router.push(`?${params.toString()}`, { scroll: false })
  }

  const isStale = model.trust.age_hours >= 24

  return (
    <button
      onClick={openModal}
      className="w-full text-left"
      style={{
        display: "flex",
        alignItems: "center",
        gap: "12px",
        padding: "12px 16px",
        backgroundColor: "var(--surface)",
        borderBottom: "1px solid var(--border)",
        cursor: "pointer",
        transition: "background-color 0.12s",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.backgroundColor  = "var(--surfaceLo)"
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor  = "var(--surface)"
      }}
    >
      {/* Confidence dot */}
      <ConfidenceDot confidence={model.trust.confidence} />

      {/* Model name + provider */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="font-outfit text-sm" style={{ color: "var(--ink)", fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
          {model.name}
        </div>
        <div className="font-outfit text-xs" style={{ color: "var(--muted)" }}>
          {model.provider} · {model.modality}
        </div>
      </div>

      {/* LIVE badge */}
      <span
        className="font-orbitron text-xs"
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "4px",
          padding: "2px 6px",
          border: "1px solid var(--accent)",
          color: "var(--accent)",
          backgroundColor: "var(--accentLt)",
          flexShrink: 0,
          opacity: isStale ? 0.5 : 1,
        }}
      >
        <span className="animate-live" style={{ width: "6px", height: "6px", borderRadius: "50%", backgroundColor: "var(--accent)", display: "inline-block" }} />
        LIVE
      </span>

      {/* Context window */}
      <span
        className="font-orbitron text-xs"
        style={{ color: "var(--dim)", flexShrink: 0, minWidth: "36px", textAlign: "right" }}
      >
        {formatContext(model.context_window)}
      </span>

      {/* Prices */}
      <div style={{ flexShrink: 0, textAlign: "right", minWidth: "100px" }}>
        <div className="font-orbitron text-xs" style={{ color: "var(--text)" }}>
          {formatPrice(model.input_price_per_m)}
          <span className="font-outfit" style={{ color: "var(--dim)", fontSize: "10px" }}> in</span>
        </div>
        <div className="font-orbitron text-xs" style={{ color: "var(--muted)" }}>
          {formatPrice(model.output_price_per_m)}
          <span className="font-outfit" style={{ color: "var(--dim)", fontSize: "10px" }}> out</span>
        </div>
      </div>
    </button>
  )
}
