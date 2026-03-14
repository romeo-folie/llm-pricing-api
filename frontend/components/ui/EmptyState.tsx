"use client"

interface EmptyStateProps {
  children: React.ReactNode
  padding?: string
}

/** Clean borderless empty state — shared across Compare, Calculator, and Changes. */
export default function EmptyState({ children, padding = "64px 24px" }: EmptyStateProps) {
  return (
    <div
      className="font-outfit text-sm"
      style={{
        textAlign: "center",
        padding,
        color: "var(--dim)",
        letterSpacing: "0.02em",
      }}
    >
      {children}
    </div>
  )
}
