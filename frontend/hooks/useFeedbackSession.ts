"use client"

import { useRef } from "react"

/**
 * Returns a stable session ID for recommendation feedback.
 * Generated once per browser session (sessionStorage) and
 * persisted across component re-renders via useRef.
 */
export function useFeedbackSession(): string {
  const ref = useRef<string | null>(null)

  if (ref.current) return ref.current

  if (typeof window !== "undefined") {
    const stored = sessionStorage.getItem("feedback_session_id")
    if (stored) {
      ref.current = stored
      return stored
    }
    const id = crypto.randomUUID()
    sessionStorage.setItem("feedback_session_id", id)
    ref.current = id
    return id
  }

  // SSR fallback — will be replaced on hydration.
  return ""
}
