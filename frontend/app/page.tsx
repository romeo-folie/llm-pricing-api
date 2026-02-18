// Landing page — full implementation in Issue #29 (blocked by #24 + #27)
// This placeholder keeps the build green while scaffolding is complete.

export default function Home() {
  return (
    <div
      className="flex flex-col items-center justify-center min-h-[60vh] gap-6 px-6 text-center"
    >
      <h1
        className="font-orbitron text-3xl font-bold"
        style={{ color: "var(--ink)" }}
      >
        LLM<span style={{ color: "var(--accent)" }}>Price</span>
      </h1>
      <p className="font-outfit text-lg max-w-md" style={{ color: "var(--muted)" }}>
        Reconciled, source-attributed LLM token pricing. Full price history. Built for agents.
      </p>
      <div className="flex gap-3 flex-wrap justify-center">
        <a
          href="/models"
          className="font-outfit text-sm font-medium px-5 py-2.5 rounded-sm"
          style={{
            backgroundColor: "var(--accent)",
            color: "var(--surfaceHi)",
            border: "1px solid var(--accentDk)",
          }}
        >
          Browse Models
        </a>
        <a
          href="/pricing"
          className="font-outfit text-sm font-medium px-5 py-2.5 rounded-sm"
          style={{
            backgroundColor: "var(--surface)",
            color: "var(--text)",
            border: "1px solid var(--border)",
          }}
        >
          View Pricing
        </a>
      </div>
    </div>
  )
}
