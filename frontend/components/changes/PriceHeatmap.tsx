"use client"

import { useMemo } from "react"
import { Treemap, ResponsiveContainer, Tooltip } from "recharts"
import type { HeatmapProviderGroup } from "@/lib/api"
import {
  heatmapToTreemap,
  deltaToBackground,
  deltaToColor,
} from "@/lib/changes-aggregation"
import { formatDelta } from "@/lib/format"
import EmptyState from "@/components/ui/EmptyState"

interface PriceHeatmapProps {
  heatmap: HeatmapProviderGroup[] | null
  onCellClick?: (provider: string) => void
}

// ─── Custom Treemap Cell ────────────────────────────────────────────────────

interface CellProps {
  x: number
  y: number
  width: number
  height: number
  depth: number
  name: string
  avgDelta?: number
  changeCount?: number
  provider?: string
}

function HeatmapCell(props: CellProps) {
  const { x, y, width, height, depth, name } = props
  const avgDelta = props.avgDelta ?? 0
  const changeCount = props.changeCount ?? 0

  // Guard: Recharts treemap layout can emit negative dimensions for tiny cells
  if (width <= 0 || height <= 0) return null

  // depth 1 = provider group, depth 2 = model leaf
  if (depth === 1) {
    return (
      <g>
        <rect
          x={x}
          y={y}
          width={width}
          height={height}
          fill="var(--surfaceLo)"
          stroke="var(--borderDk)"
          strokeWidth={1.5}
        />
        {width > 50 && height > 16 && (
          <text
            x={x + 6}
            y={y + 13}
            style={{
              fontSize: "9px",
              fontFamily: "var(--font-orbitron)",
              fill: "var(--dim)",
              textTransform: "uppercase",
              letterSpacing: "0.08em",
            }}
          >
            {name}
          </text>
        )}
      </g>
    )
  }

  if (depth !== 2) return null

  // Truncate model name to fit cell width (approx 6px per char at 9px font)
  const charBudget = Math.floor((width - 8) / 5.5)
  const displayName =
    charBudget <= 0
      ? ""
      : name.length > charBudget
        ? name.slice(0, charBudget - 1) + "\u2026"
        : name

  const showName = width > 30 && height > 20
  const showDelta = width > 36 && height > 34
  const showCount = width > 50 && height > 48

  return (
    <g style={{ cursor: "pointer" }}>
      <rect
        x={x + 0.5}
        y={y + 0.5}
        width={width - 1}
        height={height - 1}
        fill={deltaToBackground(avgDelta)}
        stroke="var(--border)"
        strokeWidth={0.5}
        rx={1}
      />
      {showName && (
        <text
          x={x + 4}
          y={y + 13}
          style={{
            fontSize: "9px",
            fontFamily: "var(--font-outfit)",
            fill: "var(--ink)",
            fontWeight: 600,
          }}
        >
          {displayName}
        </text>
      )}
      {showDelta && (
        <text
          x={x + 4}
          y={y + 25}
          style={{
            fontSize: "9px",
            fontFamily: "var(--font-orbitron)",
            fill: deltaToColor(avgDelta),
            fontWeight: 700,
          }}
        >
          {formatDelta(avgDelta)}
        </text>
      )}
      {showCount && (
        <text
          x={x + 4}
          y={y + 37}
          style={{
            fontSize: "8px",
            fontFamily: "var(--font-outfit)",
            fill: "var(--dim)",
          }}
        >
          {changeCount} change{changeCount !== 1 ? "s" : ""}
        </text>
      )}
    </g>
  )
}

// ─── Custom Tooltip ─────────────────────────────────────────────────────────

interface TooltipPayloadItem {
  payload?: {
    name?: string
    provider?: string
    avgDelta?: number
    changeCount?: number
    depth?: number
  }
}

function HeatmapTooltip({
  active,
  payload,
}: {
  active?: boolean
  payload?: TooltipPayloadItem[]
}) {
  if (!active || !payload?.length) return null
  const data = payload[0].payload
  if (!data || data.depth === 1) return null

  return (
    <div
      style={{
        backgroundColor: "var(--surface)",
        border: "1px solid var(--borderDk)",
        padding: "8px 12px",
        fontSize: "12px",
      }}
    >
      <div
        className="font-outfit"
        style={{ fontWeight: 600, color: "var(--ink)", marginBottom: "4px" }}
      >
        {data.name}
      </div>
      <div className="font-outfit" style={{ color: "var(--dim)", marginBottom: "2px" }}>
        {data.provider}
      </div>
      <div className="font-orbitron" style={{ color: deltaToColor(data.avgDelta ?? 0) }}>
        {formatDelta(data.avgDelta ?? 0)} avg
      </div>
      <div className="font-outfit" style={{ color: "var(--muted)", marginTop: "2px" }}>
        {data.changeCount} price change{data.changeCount !== 1 ? "s" : ""}
      </div>
    </div>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export default function PriceHeatmap({
  heatmap,
  onCellClick,
}: PriceHeatmapProps) {
  const treemapData = useMemo(() => {
    if (!heatmap || heatmap.length === 0) return []
    return heatmapToTreemap(heatmap)
  }, [heatmap])

  if (treemapData.length === 0) {
    return (
      <div>
        <span
          className="font-orbitron text-xs tracking-widest"
          style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
        >
          [ PRICE HEATMAP ]
        </span>
        <div
          style={{
            height: "302px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--surface)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <EmptyState padding="40px 24px">
            No price changes in this time window.
          </EmptyState>
        </div>
      </div>
    )
  }

  return (
    <div>
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
      >
        [ PRICE HEATMAP ]
      </span>
      <div
        style={{
          border: "1px solid var(--border)",
          backgroundColor: "var(--surface)",
          overflow: "hidden",
        }}
      >
        <ResponsiveContainer width="100%" height={300}>
          <Treemap
            data={treemapData}
            dataKey="size"
            nameKey="name"
            type="flat"
            content={<HeatmapCell x={0} y={0} width={0} height={0} depth={0} name="" />}
            onClick={(node) => {
              const provider =
                (node as CellProps).provider ?? (node as CellProps).name
              if ((node as CellProps).depth === 2 && onCellClick) {
                onCellClick(provider)
              }
            }}
            isAnimationActive={false}
          >
            {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
            <Tooltip content={<HeatmapTooltip />} />
          </Treemap>
        </ResponsiveContainer>
      </div>
      {/* Legend — outside border, below heatmap */}
      <div
        className="font-outfit text-xs"
        style={{
          display: "flex",
          alignItems: "center",
          gap: "16px",
          marginTop: "8px",
          color: "var(--dim)",
        }}
      >
        <span style={{ display: "inline-flex", alignItems: "center", gap: "4px" }}>
          <span
            style={{
              width: "10px",
              height: "10px",
              backgroundColor: "rgba(5, 150, 105, 0.4)",
              display: "inline-block",
            }}
          />
          Price decrease
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: "4px" }}>
          <span
            style={{
              width: "10px",
              height: "10px",
              backgroundColor: "rgba(220, 38, 38, 0.4)",
              display: "inline-block",
            }}
          />
          Price increase
        </span>
        <span style={{ marginLeft: "auto" }}>
          Cell size = change frequency
        </span>
      </div>
    </div>
  )
}
