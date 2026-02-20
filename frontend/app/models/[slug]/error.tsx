"use client"

import { useEffect } from "react"

export default function ModelDetailError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <main
      className="mx-auto max-w-3xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "80px", paddingBottom: "80px", textAlign: "center" }}
    >
      <nav className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "32px" }}>
        <a href="/models" style={{ color: "var(--muted)", textDecoration: "none" }}>Models</a>
        <span style={{ margin: "0 6px" }}>›</span>
        <span style={{ color: "var(--text)" }}>Error</span>
      </nav>
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "16px" }}
      >
        [ MODEL DETAIL ]
      </span>
      <h2
        className="font-orbitron text-lg font-bold"
        style={{ color: "var(--ink)", marginBottom: "8px" }}
      >
        Model unavailable
      </h2>
      <p
        className="font-outfit text-sm"
        style={{ color: "var(--muted)", marginBottom: "24px" }}
      >
        Unable to load this model. It may not exist or the API is temporarily unavailable.
      </p>
      <div style={{ display: "flex", gap: "12px", justifyContent: "center" }}>
        <button
          onClick={reset}
          className="font-outfit text-sm font-semibold px-5 py-2"
          style={{
            backgroundColor: "var(--accent)",
            color: "var(--surfaceHi)",
            border: "1px solid var(--accentDk)",
            cursor: "pointer",
          }}
        >
          Retry
        </button>
        <a
          href="/models"
          className="font-outfit text-sm px-5 py-2"
          style={{ color: "var(--muted)", border: "1px solid var(--border)" }}
        >
          Back to Models
        </a>
      </div>
    </main>
  )
}
