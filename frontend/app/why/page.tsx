import type { Metadata } from "next"

export const metadata: Metadata = {
  title: "Why LLMRates? — Trusted LLM Pricing vs. Spreadsheets & Single-Source Tools",
  description: "Learn how LLMRates differentiates from manual spreadsheets and single-source tools through multi-source reconciliation, immutable price history, and agent-native APIs.",
  alternates: { canonical: "/why" },
}

export default function WhyPage() {
  return (
    <main className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-20">
      <div className="max-w-3xl mx-auto">
        <h1 className="font-outfit text-4xl font-extrabold mb-8" style={{ color: "var(--ink)" }}>
          Why LLMRates?
        </h1>
        
        <p className="font-outfit text-xl mb-12 leading-relaxed" style={{ color: "var(--muted)" }}>
          In the rapidly evolving AI landscape, staying current with token pricing is a full-time job. 
          LLMRates was built to replace manual guesswork with industrial-grade data.
        </p>

        <div className="flex flex-col gap-16">
          <section>
            <h2 className="font-orbitron font-bold text-xs tracking-widest uppercase mb-4" style={{ color: "var(--accent)" }}>
              [ VS. MANUAL SPREADSHEETS ]
            </h2>
            <h3 className="font-outfit text-2xl font-bold mb-4" style={{ color: "var(--ink)" }}>
              Real-time updates vs. Stale data
            </h3>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)" }}>
              Manual spreadsheets are outdated the moment they are saved. LLMRates reconciles pricing data 
              every 5 minutes. We track thousands of models so your infra team doesn't have to manually 
              refresh CSVs every time a provider drops prices.
            </p>
          </section>

          <section>
            <h2 className="font-orbitron font-bold text-xs tracking-widest uppercase mb-4" style={{ color: "var(--purple)" }}>
              [ VS. SINGLE-SOURCE TOOLS ]
            </h2>
            <h3 className="font-outfit text-2xl font-bold mb-4" style={{ color: "var(--ink)" }}>
              Multi-source reconciliation with 2-source agreement
            </h3>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)" }}>
              Relying on a single API or documentation page is a risk. Discrepancies between providers 
              are common. LLMRates only publishes a price change when it is confirmed across at least 
              two independent sources, or after manual verification by our operators.
            </p>
          </section>

          <section>
            <h2 className="font-orbitron font-bold text-xs tracking-widest uppercase mb-4" style={{ color: "var(--green)" }}>
              [ VS. SNAPSHOT TOOLS ]
            </h2>
            <h3 className="font-outfit text-2xl font-bold mb-4" style={{ color: "var(--ink)" }}>
              Full immutable price history vs. Current price only
            </h3>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)" }}>
              Most tools show you what the price is *now*. LLMRates shows you how it got there. 
              Our TimescaleDB-backed hypertable stores every single change, allowing you to audit 
              past costs and project future trends with absolute precision.
            </p>
          </section>

          <section className="bg-surfaceLo p-8 border border-border">
            <h2 className="font-outfit text-2xl font-bold mb-4" style={{ color: "var(--ink)" }}>
              Built for the Agentic Era
            </h2>
            <p className="font-outfit text-base leading-relaxed mb-6" style={{ color: "var(--muted)" }}>
              We don't just provide data for humans; we provide it for the agents that build the modern web. 
              From lean system prompt snapshots to real-time SSE streams, our API is designed for machine 
              consumption.
            </p>
            <div className="flex gap-4">
              <a 
                href="/signup/free" 
                className="font-outfit font-semibold px-6 py-3 bg-accent text-white border border-accentDk"
              >
                Get Started Free
              </a>
              <a 
                href="/docs" 
                className="font-outfit font-semibold px-6 py-3 border border-border"
              >
                Read API Docs
              </a>
            </div>
          </section>
        </div>
      </div>
    </main>
  );
}
