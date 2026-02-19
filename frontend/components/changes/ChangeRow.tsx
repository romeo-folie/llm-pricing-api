"use client"

import type { PriceChange } from "@/lib/api"

interface ChangeRowProps {
  change: PriceChange
  isNew?: boolean
}

function formatPrice(p: number): string { return `$${p.toFixed(4)}` }
function formatDelta(pct: number): string {
  const sign = pct >= 0 ? "+" : ""
  return `${sign}${pct.toFixed(1)}%`
}
function formatRelative(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1)   return "just now"
  if (mins < 60)  return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24)   return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

const PROVIDER_COLORS: Record<string, { color: string; bg: string }> = {
  openai:    { color: "var(--accent)", bg: "var(--accentLt)" },
  anthropic: { color: "var(--blue)",   bg: "var(--blueLt)"   },
  google:    { color: "var(--blue)",   bg: "var(--blueLt)"   },
  mistral:   { color: "var(--green)",  bg: "var(--greenLt)"  },
  meta:      { color: "var(--purple)", bg: "var(--purpleLt)" },
}

function providerStyle(provider: string) {
  const key = provider.toLowerCase()
  return PROVIDER_COLORS[key] ?? { color: "var(--dim)", bg: "var(--surfaceLo)" }
}

export default function ChangeRow({ change, isNew }: ChangeRowProps) {
  const increased = change.delta_pct >= 0
  const deltaColor = increased ? "var(--red)" : "var(--green)"
  const arrow      = increased ? "▲" : "▼"
  const { color: pvColor, bg: pvBg } = providerStyle(change.provider)

  return (
    <div
      className={isNew ? "animate-flash" : ""}
      style={{
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        gap: "12px",
        padding: "12px 16px",
        borderBottom: "1px solid var(--border)",
        backgroundColor: "var(--surface)",
      }}
    >
      {/* Model + provider */}
      <div style={{ flex: 1, minWidth: "160px" }}>
        <a
          href={`/models?model=${encodeURIComponent(change.model_id)}`}
          className="font-outfit text-sm"
          style={{ color: "var(--ink)", fontWeight: 600, textDecoration: "none" }}
        >
          {change.model_name}
        </a>
        <div style={{ marginTop: "4px" }}>
          <span
            className="font-outfit text-xs"
            style={{
              padding: "1px 6px",
              border: `1px solid ${pvColor}`,
  

              color: pvColor,
              backgroundColor: pvBg,
            }}
          >
            {change.provider}
          </span>
        </div>
      </div>

      {/* Price changes */}
      <div style={{ display: "flex", flexDirection: "column", gap: "4px", minWidth: "180px" }}>
        {/* Input */}
        <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
          <span className="font-outfit text-xs" style={{ color: "var(--dim)", width: "36px" }}>in:</span>
          <span className="font-orbitron text-xs" style={{ color: "var(--muted)" }}>
            {formatPrice(change.old_input_price)}
          </span>
          <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>→</span>
          <span className="font-orbitron text-xs" style={{ color: "var(--text)" }}>
            {formatPrice(change.new_input_price)}
          </span>
        </div>
        {/* Output */}
        <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
          <span className="font-outfit text-xs" style={{ color: "var(--dim)", width: "36px" }}>out:</span>
          <span className="font-orbitron text-xs" style={{ color: "var(--muted)" }}>
            {formatPrice(change.old_output_price)}
          </span>
          <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>→</span>
          <span className="font-orbitron text-xs" style={{ color: "var(--text)" }}>
            {formatPrice(change.new_output_price)}
          </span>
        </div>
      </div>

      {/* Delta */}
      <span
        className="font-orbitron text-sm"
        style={{
          color: deltaColor,
          minWidth: "64px",
          textAlign: "right",
          fontWeight: 700,
        }}
      >
        {arrow} {formatDelta(change.delta_pct)}
      </span>

      {/* Source + time */}
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: "4px", minWidth: "72px" }}>
        <span
          className="font-outfit text-xs"
          style={{
            padding: "1px 6px",
            border: "1px solid var(--border)",


            color: "var(--blue)",
            backgroundColor: "var(--blueLt)",
          }}
        >
          {change.source}
        </span>
        <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
          {formatRelative(change.changed_at)}
        </span>
      </div>
    </div>
  )
}
