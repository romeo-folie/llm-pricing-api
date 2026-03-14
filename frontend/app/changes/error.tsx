"use client"

import { useEffect } from "react"

export default function ChangesError({
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
    <div
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "80px", paddingBottom: "80px", textAlign: "center" }}
    >
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "16px" }}
      >
        PRICE CHANGES
      </span>
      <h2
        className="font-orbitron text-lg font-bold"
        style={{ color: "var(--ink)", marginBottom: "8px" }}
      >
        Feed unavailable
      </h2>
      <p
        className="font-outfit text-sm"
        style={{ color: "var(--muted)", marginBottom: "24px" }}
      >
        Unable to load price change data. The API may be temporarily unavailable.
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
          href="/"
          className="font-outfit text-sm px-5 py-2"
          style={{ color: "var(--muted)", border: "1px solid var(--border)" }}
        >
          Home
        </a>
      </div>
    </div>
  )
}
