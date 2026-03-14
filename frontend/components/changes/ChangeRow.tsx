"use client"

import type { PriceChange } from "@/lib/api"
import { formatPrice, formatDelta, formatRelative, formatSourceName } from "@/lib/format"
import { providerStyle } from "@/lib/provider-colors"

interface ChangeRowProps {
  change: PriceChange
  isNew?: boolean
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
        backgroundColor: "transparent",
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
          {formatSourceName(change.source)}
        </span>
        <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
          {formatRelative(change.changed_at)}
        </span>
      </div>
    </div>
  )
}
