"use client"

import { useEffect } from "react"

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  const isApiError = error.message?.includes("API error")

  return (
    <div
      className="mx-auto max-w-2xl px-4"
      style={{ paddingTop: "120px", paddingBottom: "120px", textAlign: "center" }}
    >
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "24px" }}
      >
        [ SYSTEM ERROR ]
      </span>
      <h2
        className="font-orbitron text-xl font-bold"
        style={{ color: "var(--ink)", marginBottom: "12px" }}
      >
        Something went wrong
      </h2>
      <p
        className="font-outfit text-sm"
        style={{ color: "var(--muted)", marginBottom: "8px", maxWidth: "360px", margin: "0 auto 8px" }}
      >
        {isApiError
          ? "The pricing API is currently unavailable. Data will appear once connectivity is restored."
          : "An unexpected error occurred. Please try again."}
      </p>
      {error.digest && (
        <p
          className="font-orbitron text-xs"
          style={{ color: "var(--dim)", marginBottom: "32px", marginTop: "8px" }}
        >
          ref: {error.digest}
        </p>
      )}
      <div style={{ display: "flex", gap: "12px", justifyContent: "center", marginTop: "32px" }}>
        <button
          onClick={reset}
          className="font-outfit text-sm font-semibold px-6 py-2.5"
          style={{
            backgroundColor: "var(--accent)",
            color: "var(--surfaceHi)",
            border: "1px solid var(--accentDk)",
            cursor: "pointer",
          }}
        >
          Try again
        </button>
        <a
          href="/"
          className="font-outfit text-sm px-6 py-2.5"
          style={{
            color: "var(--muted)",
            border: "1px solid var(--border)",
          }}
        >
          Return home
        </a>
      </div>
    </div>
  )
}
