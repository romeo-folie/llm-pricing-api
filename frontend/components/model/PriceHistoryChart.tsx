"use client"

import { useState } from "react"
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts"
import type { PriceHistoryEntry } from "@/lib/api"

interface PriceHistoryChartProps {
  history: PriceHistoryEntry[]
}

type Range = "7d" | "30d" | "90d" | "all"

const RANGES: { label: string; value: Range }[] = [
  { label: "7d",  value: "7d"  },
  { label: "30d", value: "30d" },
  { label: "90d", value: "90d" },
  { label: "All", value: "all" },
]

function filterByRange(entries: PriceHistoryEntry[], range: Range): PriceHistoryEntry[] {
  if (range === "all") return entries
  const days = range === "7d" ? 7 : range === "30d" ? 30 : 90
  const cutoff = new Date()
  cutoff.setDate(cutoff.getDate() - days)
  return entries.filter((e) => new Date(e.timestamp) >= cutoff)
}

function formatDate(ts: string): string {
  return new Date(ts).toLocaleDateString("en-US", { month: "short", day: "numeric" })
}

export default function PriceHistoryChart({ history }: PriceHistoryChartProps) {
  const [range, setRange] = useState<Range>("30d")

  const filtered = filterByRange(history, range)
  const data = filtered.map((e) => ({
    date: formatDate(e.timestamp),
    input:  e.input_price_per_m,
    output: e.output_price_per_m,
  }))

  if (history.length === 0) {
    return (
      <div
        className="font-outfit text-sm"
        style={{
          textAlign: "center",
          padding: "32px",
          color: "var(--muted)",
          border: "1px solid var(--border)",
          borderRadius: "2px",
        }}
      >
        No price history available.
      </div>
    )
  }

  if (filtered.length === 0) {
    return (
      <div>
        <div style={{ display: "flex", gap: "4px", marginBottom: "16px" }}>
          {RANGES.map((r) => (
            <button
              key={r.value}
              onClick={() => setRange(r.value)}
              className="font-orbitron text-xs"
              style={{
                padding: "4px 10px",
                border: "1px solid var(--border)",
                borderRadius: "2px",
                backgroundColor: range === r.value ? "var(--accent)" : "var(--surface)",
                color: range === r.value ? "white" : "var(--muted)",
                cursor: "pointer",
              }}
            >
              {r.label}
            </button>
          ))}
        </div>
        <div
          className="font-outfit text-sm"
          style={{
            textAlign: "center",
            padding: "32px",
            color: "var(--muted)",
            border: "1px solid var(--border)",
            borderRadius: "2px",
          }}
        >
          No price history in this range.
        </div>
      </div>
    )
  }

  return (
    <div>
      {/* Range selector */}
      <div style={{ display: "flex", gap: "4px", marginBottom: "16px" }}>
        {RANGES.map((r) => (
          <button
            key={r.value}
            onClick={() => setRange(r.value)}
            className="font-orbitron text-xs"
            style={{
              padding: "4px 10px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: range === r.value ? "var(--accent)" : "var(--surface)",
              color: range === r.value ? "white" : "var(--muted)",
              cursor: "pointer",
            }}
          >
            {r.label}
          </button>
        ))}
      </div>

      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={data} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
          <XAxis
            dataKey="date"
            tick={{ fontSize: 10, fontFamily: "monospace", fill: "var(--dim)" }}
            tickLine={false}
            axisLine={{ stroke: "var(--border)" }}
          />
          <YAxis
            tick={{ fontSize: 10, fontFamily: "monospace", fill: "var(--dim)" }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => `$${(v as number).toFixed(2)}`}
            width={52}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--surface)",
              border: "1px solid var(--borderDk)",
              borderRadius: "2px",
              fontSize: "12px",
              fontFamily: "monospace",
            }}
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            formatter={(value: any, name: any) => [
              typeof value === "number" ? `$${(value as number).toFixed(4)}` : "—",
              name === "input" ? "Input /1M" : "Output /1M",
            ]}
          />
          <Legend
            wrapperStyle={{ fontSize: "11px", fontFamily: "monospace" }}
            formatter={(value) => (value === "input" ? "Input /1M" : "Output /1M")}
          />
          <Line
            type="monotone"
            dataKey="input"
            stroke="var(--blue)"
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3 }}
          />
          <Line
            type="monotone"
            dataKey="output"
            stroke="var(--accent)"
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
