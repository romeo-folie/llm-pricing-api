import type { Metadata } from "next"

export const metadata: Metadata = {
  title: "404 — Page Not Found | LLMPrice",
}

export default function NotFound() {
  return (
    <main
      className="mx-auto max-w-2xl px-4"
      style={{ paddingTop: "120px", paddingBottom: "120px", textAlign: "center" }}
    >
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{ color: "var(--dim)", display: "block", marginBottom: "24px" }}
      >
        [ 404 ]
      </span>
      <h1
        className="font-orbitron text-3xl font-bold"
        style={{ color: "var(--ink)", marginBottom: "12px" }}
      >
        Page not found
      </h1>
      <p
        className="font-outfit text-sm"
        style={{ color: "var(--muted)", marginBottom: "32px" }}
      >
        The page you&apos;re looking for doesn&apos;t exist or has been moved.
      </p>
      <div style={{ display: "flex", gap: "12px", justifyContent: "center" }}>
        <a
          href="/"
          className="font-outfit text-sm font-semibold px-6 py-2.5"
          style={{
            backgroundColor: "var(--accent)",
            color: "var(--surfaceHi)",
            border: "1px solid var(--accentDk)",
          }}
        >
          Return home
        </a>
        <a
          href="/models"
          className="font-outfit text-sm px-6 py-2.5"
          style={{
            color: "var(--muted)",
            border: "1px solid var(--border)",
          }}
        >
          Browse models
        </a>
      </div>
    </main>
  )
}
