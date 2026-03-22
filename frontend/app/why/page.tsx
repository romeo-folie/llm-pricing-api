import type { Metadata } from "next"

export const metadata: Metadata = {
  title: "Why LLMRates? — Trusted LLM Pricing vs. Spreadsheets & Single-Source Tools",
  description: "Learn how LLMRates differentiates from manual spreadsheets and single-source tools through multi-source reconciliation, immutable price history, and agent-native APIs.",
  alternates: { canonical: "/why" },
}

export default function WhyPage() {
  return (
    <main className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-16 lg:py-24">
      <div className="max-w-3xl mx-auto flex flex-col gap-12">
        
        {/* Header */}
        <header className="flex flex-col gap-6 animate-wireframe-fade" style={{ animationDelay: '0.3s' }}>
          <h1 className="font-outfit text-4xl lg:text-5xl font-bold tracking-tight" style={{ color: "var(--ink)" }}>
            Why <span style={{ color: "var(--accent)" }}>LLMRates</span>?
          </h1>
          <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)" }}>
            In the rapidly evolving AI landscape, staying current with token pricing is a full-time job. 
            LLMRates was built to replace manual guesswork with industrial-grade data.
          </p>
        </header>

        <div className="flex flex-col gap-16 py-8">
          {/* Section 1 */}
          <section className="animate-wireframe-fade" style={{ animationDelay: '0.4s' }}>
            <div className="flex flex-col gap-3 mb-6">
              <span className="font-orbitron font-bold text-[10px] tracking-[0.25em] uppercase opacity-60" style={{ color: "var(--accent)" }}>
                [ VS. MANUAL SPREADSHEETS ]
              </span>
              <h2 className="font-outfit text-2xl lg:text-3xl font-bold tracking-tight" style={{ color: "var(--ink)" }}>
                Real-time updates vs. Stale data
              </h2>
            </div>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)", maxWidth: "65ch" }}>
              Manual spreadsheets are outdated the moment they are saved. LLMRates reconciles pricing data 
              every 5 minutes. We track hundreds of models so your infra team doesn't have to manually 
              refresh CSVs every time a provider drops prices.
            </p>
          </section>

          {/* Section 2 */}
          <section className="animate-wireframe-fade" style={{ animationDelay: '0.5s' }}>
            <div className="flex flex-col gap-3 mb-6">
              <span className="font-orbitron font-bold text-[10px] tracking-[0.25em] uppercase opacity-60" style={{ color: "var(--purple)" }}>
                [ VS. SINGLE-SOURCE TOOLS ]
              </span>
              <h2 className="font-outfit text-2xl lg:text-3xl font-bold tracking-tight" style={{ color: "var(--ink)" }}>
                Multi-source reconciliation with 2-source agreement
              </h2>
            </div>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)", maxWidth: "65ch" }}>
              Relying on a single API or documentation page is a risk. Discrepancies between providers 
              are common. LLMRates only publishes a price change when it is confirmed across at least 
              two independent sources, or after manual verification by our operators.
            </p>
          </section>

          {/* Section 3 */}
          <section className="animate-wireframe-fade" style={{ animationDelay: '0.6s' }}>
            <div className="flex flex-col gap-3 mb-6">
              <span className="font-orbitron font-bold text-[10px] tracking-[0.25em] uppercase opacity-60" style={{ color: "var(--green)" }}>
                [ VS. SNAPSHOT TOOLS ]
              </span>
              <h2 className="font-outfit text-2xl lg:text-3xl font-bold tracking-tight" style={{ color: "var(--ink)" }}>
                Full immutable price history vs. Current price only
              </h2>
            </div>
            <p className="font-outfit text-base leading-relaxed" style={{ color: "var(--muted)", maxWidth: "65ch" }}>
              Most tools show you what the price is *now*. LLMRates shows you how it got there. 
              Our database hypertable stores every single change, allowing you to audit 
              past costs and project future trends with absolute precision.
            </p>
          </section>

          {/* Call-to-action */}
          <section 
            className="animate-wireframe-fade p-8 lg:p-12 border-l-4 border-dashed" 
            style={{ 
              borderColor: "var(--border)",
              backgroundColor: "var(--surfaceLo)",
              animationDelay: '0.7s'
            }}
          >
            <h2 className="font-outfit text-2xl font-bold mb-4" style={{ color: "var(--ink)" }}>
              Built for the Agentic Era
            </h2>
            <p className="font-outfit text-base leading-relaxed mb-8" style={{ color: "var(--muted)", maxWidth: "60ch" }}>
              We don't just provide data for humans; we provide it for the agents that build the modern web. 
              From lean system prompt snapshots to real-time SSE streams, our API is designed for machine 
              consumption.
            </p>
            <div className="flex gap-4 flex-wrap">
              <a 
                href="/signup" 
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
