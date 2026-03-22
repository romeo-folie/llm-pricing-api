import type { Metadata } from "next"
import { Suspense } from "react"
import SignupFlow from "./SignupFlow"
import { Zap, Key, Cpu, ShieldOff } from "lucide-react"

export const metadata: Metadata = {
  title: "Free API Key — LLMRates",
  description:
    "Get a free API key for LLMRates. No credit card. Verify your email and start querying live LLM pricing data.",
  robots: { index: true, follow: true },
}

export default function SignupPage() {
  return (
    <main className="signup-page">
      <div className="signup-shell">
        {/* ── Left panel: brand/value prop ─────────────────────────────── */}
        <aside className="signup-aside animate-wireframe-fade" style={{ animationDelay: "0.2s" }}>
          <div className="signup-aside-body">
            <h1 className="signup-aside-heading">
              Live pricing.
              <br />
              <span className="signup-aside-accent">Free.</span>
            </h1>
            <p className="signup-aside-desc">
              Query real-time LLM pricing data across every major provider.
              One key. 100 requests/day on the free tier.
            </p>

            <ul className="signup-aside-features">
              {FEATURES.map((f, i) => (
                <li 
                  key={f.label} 
                  className="signup-aside-feature animate-reveal-nav-link"
                  style={{ animationDelay: `${0.8 + i * 0.1}s` }}
                >
                  <span className="signup-aside-feature-icon" aria-hidden="true" style={{ color: "var(--accent)" }}>
                    <f.icon size={16} strokeWidth={2.5} />
                  </span>
                  <span>{f.label}</span>
                </li>
              ))}
            </ul>
          </div>
        </aside>

        {/* ── Right panel: interactive flow ────────────────────────────── */}
        <section 
          className="signup-main animate-reveal-card" 
          aria-label="Get your free API key"
          style={{ animationDelay: "0.4s" }}
        >
          <Suspense fallback={
            <div className="signup-flow-center" role="status" aria-label="Loading signup…">
              <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
              <span className="sr-only">Loading signup…</span>
            </div>
          }>
            <SignupFlow />
          </Suspense>
        </section>
      </div>
    </main>
  )
}

const FEATURES = [
  { icon: Zap, label: "Real-time pricing across 200+ models" },
  { icon: Key, label: "API key in under 60 seconds" },
  { icon: Cpu, label: "REST API — works in any language" },
  { icon: ShieldOff, label: "No credit card required" },
]
