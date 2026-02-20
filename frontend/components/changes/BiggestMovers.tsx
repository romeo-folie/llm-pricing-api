"use client"

import type { TopMover } from "@/lib/api"
import { deltaToColor } from "@/lib/changes-aggregation"
import { formatDelta } from "@/lib/format"
import { providerStyle } from "@/lib/provider-colors"
import EmptyState from "@/components/ui/EmptyState"

interface BiggestMoversProps {
  topMovers: TopMover[] | null
}

export default function BiggestMovers({ topMovers }: BiggestMoversProps) {
  const movers = topMovers ?? []

  return (
    <div>
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
      >
        [ BIGGEST MOVERS ]
      </span>
      <div
        style={{
          border: "1px solid var(--border)",
          backgroundColor: "var(--surface)",
          padding: "12px",
          minHeight: "200px",
        }}
      >
        {movers.length === 0 ? (
          <EmptyState padding="40px 12px">
            No movers in this window.
          </EmptyState>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: "0px" }}>
            {movers.map((mover, i) => {
              const increased = mover.delta_pct >= 0
              const arrow = increased ? "\u25B2" : "\u25BC"
              const { color: pvColor, bg: pvBg } = providerStyle(mover.provider)

              return (
                <div
                  key={`${mover.model_id}-${mover.confirmed_at}`}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    padding: "10px 4px",
                    borderBottom:
                      i < movers.length - 1
                        ? "1px solid var(--border)"
                        : "none",
                  }}
                >
                  {/* Rank */}
                  <span
                    className="font-orbitron text-sm"
                    style={{
                      color: "var(--dim)",
                      width: "18px",
                      textAlign: "center",
                      flexShrink: 0,
                    }}
                  >
                    {i + 1}
                  </span>

                  {/* Model + provider */}
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <a
                      href={`/models?model=${encodeURIComponent(mover.model_id)}`}
                      className="font-outfit text-sm"
                      style={{
                        color: "var(--ink)",
                        fontWeight: 600,
                        textDecoration: "none",
                        display: "block",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {mover.model_slug}
                    </a>
                    <span
                      className="font-outfit"
                      style={{
                        fontSize: "10px",
                        padding: "0px 4px",
                        border: `1px solid ${pvColor}`,
                        color: pvColor,
                        backgroundColor: pvBg,
                        display: "inline-block",
                        marginTop: "2px",
                      }}
                    >
                      {mover.provider}
                    </span>
                  </div>

                  {/* Delta */}
                  <span
                    className="font-orbitron text-sm"
                    style={{
                      color: deltaToColor(mover.delta_pct),
                      fontWeight: 700,
                      flexShrink: 0,
                      textAlign: "right",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {arrow} {formatDelta(mover.delta_pct)}
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
