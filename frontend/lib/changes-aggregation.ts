import type { HeatmapProviderGroup } from "./api"

// ─── Types ──────────────────────────────────────────────────────────────────

export type TimeWindow = "24h" | "7d" | "30d"

/** Leaf node in the treemap — represents a single model within a provider group. */
export interface TreemapModelNode {
  name: string
  provider: string
  size: number       // change frequency (count) — drives cell area
  avgDelta: number   // average delta_pct — drives cell color
  changeCount: number
  model_id: string
  // Index signature required by Recharts TreemapDataType
  [key: string]: unknown
}

/** Parent node in the treemap — represents a provider containing model children. */
export interface TreemapProviderGroup {
  name: string
  children: TreemapModelNode[]
  // Index signature required by Recharts TreemapDataType
  [key: string]: unknown
}

// ─── Treemap Data Transform ─────────────────────────────────────────────────

/**
 * Transform backend HeatmapProviderGroup[] into the nested hierarchy
 * that Recharts Treemap expects.
 */
export function heatmapToTreemap(
  groups: HeatmapProviderGroup[],
): TreemapProviderGroup[] {
  return groups.map((g) => ({
    name: g.provider,
    children: g.models.map((m) => ({
      name: m.model_slug,
      provider: g.provider,
      size: m.change_count,
      avgDelta: m.avg_delta_pct,
      changeCount: m.change_count,
      model_id: m.model_id,
    })),
  }))
}

// ─── Color Utilities ────────────────────────────────────────────────────────

/** Map a delta_pct to a foreground color for text. */
export function deltaToColor(delta: number): string {
  if (Math.abs(delta) < 0.5) return "var(--dim)"
  return delta > 0 ? "var(--red)" : "var(--green)"
}

/**
 * Map a delta_pct to a background color for treemap cells.
 * Intensity scales with magnitude, capped at 30% delta.
 */
export function deltaToBackground(delta: number): string {
  if (Math.abs(delta) < 0.5) return "rgba(120, 113, 108, 0.08)" // subtle neutral
  const abs = Math.min(Math.abs(delta), 30)
  const opacity = 0.12 + (abs / 30) * 0.5
  if (delta > 0) return `rgba(220, 38, 38, ${opacity.toFixed(2)})`   // red
  return `rgba(5, 150, 105, ${opacity.toFixed(2)})`                   // green
}
