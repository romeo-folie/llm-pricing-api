import type { Metadata } from "next"
import Link from "next/link"
import { USE_CASES } from "@/lib/use-cases"

export const metadata: Metadata = {
  title: "Best Models For... | LLMRates",
  description:
    "Find the best AI model for your use case — coding, reasoning, finance, writing, video, or agentic workflows. Benchmark-backed rankings.",
}

export default function BestForPage() {
  return (
    <main className="mx-auto max-w-[1280px] px-4 py-10">
      <h1
        className="font-orbitron text-2xl font-bold tracking-tight mb-2 animate-wireframe-fade"
        style={{ color: "var(--ink)" }}
      >
        Best Models For&hellip;
      </h1>
      <p
        className="font-outfit text-sm mb-8 animate-wireframe-fade"
        style={{ color: "var(--muted)", animationDelay: "0.1s" }}
      >
        Choose a use case to see benchmark-backed model rankings.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {USE_CASES.map((uc, i) => (
          <Link
            key={uc.slug}
            href={`/best-for/${uc.slug}`}
            className="group block border p-5 transition-colors animate-wireframe-fade"
            style={{
              borderColor: "var(--border)",
              backgroundColor: "var(--surface)",
              animationDelay: `${0.15 + i * 0.05}s`,
            }}
          >
            <div className="flex items-start justify-between mb-3">
              <span className="text-2xl" role="img" aria-label={uc.label}>
                {uc.icon}
              </span>
              <span
                className="font-outfit text-sm transition-transform group-hover:translate-x-1"
                style={{ color: "var(--muted)" }}
              >
                &rarr;
              </span>
            </div>
            <h2
              className="font-orbitron text-base font-semibold mb-1"
              style={{ color: "var(--ink)" }}
            >
              {uc.label}
            </h2>
            <p className="font-outfit text-sm" style={{ color: "var(--muted)" }}>
              {uc.description}
            </p>
          </Link>
        ))}
      </div>
    </main>
  )
}
