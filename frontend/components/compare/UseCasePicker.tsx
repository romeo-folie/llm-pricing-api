"use client"

import { useCallback, useRef } from "react"
import { USE_CASES } from "@/lib/use-cases"

interface UseCasePickerProps {
  selected: string | null
  onChange: (slug: string) => void
}

export default function UseCasePicker({ selected, onChange }: UseCasePickerProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const items = containerRef.current?.querySelectorAll<HTMLButtonElement>("[role='radio']")
      if (!items) return
      const currentIdx = Array.from(items).findIndex(
        (el) => el.getAttribute("aria-checked") === "true",
      )

      let nextIdx = currentIdx
      if (e.key === "ArrowRight" || e.key === "ArrowDown") {
        e.preventDefault()
        nextIdx = (currentIdx + 1) % items.length
      } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
        e.preventDefault()
        nextIdx = (currentIdx - 1 + items.length) % items.length
      }

      if (nextIdx !== currentIdx) {
        items[nextIdx].focus()
        onChange(USE_CASES[nextIdx].slug)
      }
    },
    [onChange],
  )

  return (
    <div
      ref={containerRef}
      role="radiogroup"
      aria-label="Select a use case"
      onKeyDown={handleKeyDown}
      className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3"
    >
      {USE_CASES.map((uc) => {
        const isSelected = selected === uc.slug
        return (
          <button
            key={uc.slug}
            role="radio"
            aria-checked={isSelected}
            tabIndex={isSelected || (!selected && uc === USE_CASES[0]) ? 0 : -1}
            onClick={() => onChange(uc.slug)}
            className="text-left p-4 border transition-colors"
            style={{
              borderColor: isSelected ? "var(--accent)" : "var(--border)",
              borderWidth: "1px",
              borderRadius: 0,
              backgroundColor: isSelected ? "var(--accentLt)" : "var(--bg)",
              cursor: "pointer",
              transitionProperty: "border-color, background-color",
              transitionDuration: "150ms",
              transitionTimingFunction: "ease",
            }}
            onMouseEnter={(e) => {
              if (!isSelected) {
                e.currentTarget.style.backgroundColor = "var(--surface)"
              }
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = isSelected
                ? "var(--accentLt)"
                : "var(--bg)"
            }}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex-1 min-w-0">
                <span
                  className="font-outfit text-sm font-semibold tracking-wide block mb-1"
                  style={{ color: "var(--ink)" }}
                >
                  {uc.label}
                </span>
                <p
                  className="font-outfit text-xs"
                  style={{ color: "var(--muted)", lineHeight: "1.4" }}
                >
                  {uc.description}
                </p>
              </div>
              <span className="text-lg flex-shrink-0" aria-hidden="true">{uc.icon}</span>
            </div>
          </button>
        )
      })}
    </div>
  )
}
