"use client"

import { useId } from "react"

interface EmptyStateProps {
  children: React.ReactNode
  padding?: string
}

/** Isometric grid background empty state — shared across Compare, Calculator, and Changes. */
export default function EmptyState({ children, padding = "64px 24px" }: EmptyStateProps) {
  const gridId = useId()

  return (
    <div
      className="font-outfit text-sm"
      style={{
        position: "relative",
        textAlign: "center",
        padding,
        border: "1px solid var(--border)",
        color: "var(--muted)",
        overflow: "hidden",
      }}
    >
      <svg
        aria-hidden="true"
        style={{
          position: "absolute",
          inset: 0,
          width: "100%",
          height: "100%",
          opacity: 0.06,
          pointerEvents: "none",
        }}
      >
        <defs>
          <pattern
            id={gridId}
            x="0"
            y="0"
            width="40"
            height="40"
            patternUnits="userSpaceOnUse"
            patternTransform="rotate(-30) skewX(20)"
          >
            <path d="M 40 0 L 0 0 0 40" fill="none" stroke="var(--borderDk)" strokeWidth="0.5" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill={`url(#${gridId})`} />
      </svg>
      <span style={{ position: "relative" }}>{children}</span>
    </div>
  )
}
