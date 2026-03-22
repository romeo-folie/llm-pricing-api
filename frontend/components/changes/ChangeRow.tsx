"use client"

import type { PriceChange } from "@/lib/api"
import { formatPrice, formatDelta, formatRelative, formatSourceName } from "@/lib/format"
import { providerStyle } from "@/lib/provider-colors"
import { SiteBadge } from "@/components/ui/SiteBadge"

interface ChangeRowProps {
  change: PriceChange
  isNew?: boolean
  index?: number
}

export default function ChangeRow({ change, isNew, index = 0 }: ChangeRowProps) {
  const increased = change.delta_pct >= 0
  const deltaColor = increased ? "var(--red)" : "var(--green)"
  const arrow      = increased ? "▲" : "▼"
  const { color: pvColor, bg: pvBg } = providerStyle(change.provider)

  const drawDelay = 0.8 + Math.min(index, 20) * 0.05
  const fadeDelay = 1.3 + Math.min(index, 20) * 0.05

  return (
    <div
      className={`${isNew ? "animate-flash " : ""}animate-draw-border-b`}
      style={{
        backgroundColor: "transparent",
        transition: "background-color 0.12s",
        "--draw-delay": `${drawDelay}s`
      } as React.CSSProperties}
      onMouseEnter={(e) => {
        e.currentTarget.style.backgroundColor = "var(--surface)"
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor = "transparent"
      }}
    >
      <div
        className="animate-wireframe-fade"
        style={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: "12px",
          padding: "12px 16px",
          animationDelay: `${fadeDelay}s`,
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
          <SiteBadge
            label={change.provider}
            {...providerStyle(change.provider)}
          />
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
        <SiteBadge
          label={formatSourceName(change.source)}
          color="var(--blue)"
          bg="var(--blueLt)"
        />
        <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
          {formatRelative(change.changed_at)}
        </span>
      </div>
      </div>
    </div>
  )
}
