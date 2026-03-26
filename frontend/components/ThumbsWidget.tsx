"use client"

import { useState, useCallback } from "react"
import { useFeedbackSession } from "../hooks/useFeedbackSession"

interface ThumbsWidgetProps {
  useCase: string
  modelSlug: string
}

type FeedbackState = "idle" | "up" | "down" | "sending"

export default function ThumbsWidget({ useCase, modelSlug }: ThumbsWidgetProps) {
  const sessionId = useFeedbackSession()
  const [state, setState] = useState<FeedbackState>("idle")

  const send = useCallback(
    async (signal: 1 | -1) => {
      if (state === "sending") return
      const target: FeedbackState = signal === 1 ? "up" : "down"

      // Optimistic highlight.
      setState(target)

      try {
        const res = await fetch("/api/feedback", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            use_case: useCase,
            model_slug: modelSlug,
            signal,
            session_id: sessionId,
          }),
        })

        if (!res.ok) {
          // Revert on error.
          setState("idle")
          return
        }

        // Brief confirmation — revert to idle after 1s.
        setTimeout(() => setState("idle"), 1000)
      } catch {
        setState("idle")
      }
    },
    [useCase, modelSlug, sessionId, state]
  )

  return (
    <div className="inline-flex items-center gap-1">
      <button
        type="button"
        aria-label="Thumbs up"
        onClick={() => send(1)}
        disabled={state === "sending"}
        className="text-sm px-1.5 py-0.5 border transition-colors"
        style={{
          borderColor:
            state === "up" ? "var(--green, #22c55e)" : "var(--border)",
          color:
            state === "up" ? "var(--green, #22c55e)" : "var(--muted)",
          backgroundColor:
            state === "up" ? "rgba(34,197,94,0.1)" : "transparent",
          cursor: state === "sending" ? "not-allowed" : "pointer",
          borderRadius: 0,
        }}
      >
        &#x1F44D;
      </button>
      <button
        type="button"
        aria-label="Thumbs down"
        onClick={() => send(-1)}
        disabled={state === "sending"}
        className="text-sm px-1.5 py-0.5 border transition-colors"
        style={{
          borderColor:
            state === "down" ? "var(--red, #ef4444)" : "var(--border)",
          color:
            state === "down" ? "var(--red, #ef4444)" : "var(--muted)",
          backgroundColor:
            state === "down" ? "rgba(239,68,68,0.1)" : "transparent",
          cursor: state === "sending" ? "not-allowed" : "pointer",
          borderRadius: 0,
        }}
      >
        &#x1F44E;
      </button>
    </div>
  )
}
