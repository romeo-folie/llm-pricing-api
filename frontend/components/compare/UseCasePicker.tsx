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
      data-uc-grid=""
      className="grid gap-3"
      style={{
        gridTemplateColumns: "repeat(3, 1fr)",
      }}
    >
      <style>{`
        @media (max-width: 768px) {
          [data-uc-grid] { grid-template-columns: repeat(2, 1fr) !important; }
        }
        @media (max-width: 480px) {
          [data-uc-grid] { grid-template-columns: 1fr !important; }
        }
      `}</style>
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
              borderWidth: isSelected ? "2px" : "1px",
              backgroundColor: isSelected ? "#E1F5EE" : "var(--bg)",
              cursor: "pointer",
              padding: isSelected ? "15px" : "16px", // compensate for 2px border
              transitionProperty: "border-color, background-color",
              transitionDuration: "150ms",
              transitionTimingFunction: "ease",
            }}
          >
            <div className="flex items-center gap-2 mb-1">
              <span className="text-lg" aria-hidden="true">{uc.icon}</span>
              <span
                className="font-orbitron text-sm font-semibold tracking-wide"
                style={{ color: "var(--ink)" }}
              >
                {uc.label}
              </span>
            </div>
            <p
              className="font-outfit text-xs"
              style={{ color: "var(--muted)", lineHeight: "1.4" }}
            >
              {uc.description}
            </p>
          </button>
        )
      })}
    </div>
  )
}
