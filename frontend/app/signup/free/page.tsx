import type { Metadata } from "next";
import { Suspense } from "react";
import SignupFlow from "./SignupFlow";

export const metadata: Metadata = {
  title: "Free API Key — LLMRates",
  description:
    "Get a free API key for LLMRates. No credit card. Verify your email and start querying live LLM pricing data.",
  robots: { index: true, follow: true },
};

export default function SignupPage() {
  return (
    <main className="signup-page">
      <div className="signup-shell">
        {/* ── Left panel: brand/value prop ─────────────────────────────── */}
        <aside className="signup-aside">
          <a href="/" className="signup-logo" aria-label="LLMRates home">
            <span className="signup-logo-mark">LLM</span>
            <span className="signup-logo-dot">·</span>
            <span className="signup-logo-sub">Rates</span>
          </a>

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
              {FEATURES.map((f) => (
                <li key={f.label} className="signup-aside-feature">
                  <span className="signup-aside-feature-icon" aria-hidden="true">
                    {f.icon}
                  </span>
                  <span>{f.label}</span>
                </li>
              ))}
            </ul>
          </div>

          <div className="signup-aside-endpoint">
            <span className="signup-aside-endpoint-method">GET</span>
            <code className="signup-aside-endpoint-path">/v1/models</code>
          </div>
        </aside>

        {/* ── Right panel: interactive flow ────────────────────────────── */}
        <section className="signup-main" aria-label="Get your free API key">
          <Suspense fallback={
            <div className="signup-flow-center" role="status">
              <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
            </div>
          }>
            <SignupFlow />
          </Suspense>
        </section>
      </div>
    </main>
  );
}

const FEATURES = [
  { icon: "⚡", label: "Real-time pricing across 200+ models" },
  { icon: "🔑", label: "API key in under 60 seconds" },
  { icon: "📦", label: "REST API — works in any language" },
  { icon: "🛡", label: "No credit card required" },
];
