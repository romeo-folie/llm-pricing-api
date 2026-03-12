import type { Metadata } from "next"
import { safeJsonLd } from "@/lib/utils"

export const revalidate = false

export const metadata: Metadata = {
  title: "API Features — LLM Pricing Data for Developers",
  description:
    "Explore LLMRates API capabilities: reconciled pricing, full history, agent-native endpoints, webhooks, and real-time change streaming.",
  alternates: { canonical: "/pricing" },
  openGraph: {
    title: "LLMRates API Features",
    description:
      "Reconciled LLM pricing data with history, streaming updates, model recommendations, and webhook integrations.",
  },
}

const FEATURE_GROUPS = [
  {
    title: "Core Pricing API",
    items: [
      "GET /v1/models — full model catalogue",
      "GET /v1/models/:id — model detail",
      "GET /v1/providers — provider catalogue",
      "GET /v1/compare — side-by-side model comparison",
      "GET /v1/changes — recent price deltas",
    ],
  },
  {
    title: "History & Context",
    items: [
      "GET /v1/models/:id/history — full price timeline",
      "GET /v1/context — compact context snapshot for system prompts",
      "GET /v1/recommend — model recommendations by task/context/price",
    ],
  },
  {
    title: "Agent & Automation",
    items: [
      "POST /v1/ask — natural language pricing queries",
      "GET /v1/stream/changes — SSE stream with replay semantics",
      "POST /v1/webhooks — signed webhook subscriptions",
      "DELETE /v1/webhooks/:id — webhook removal",
    ],
  },
]

const FAQ = [
  {
    q: "How are prices verified?",
    a: "Data is scraped from multiple sources and reconciled before publishing. Divergent values are flagged for review.",
  },
  {
    q: "How fast do changes appear?",
    a: "Price updates are ingested continuously and surfaced through API endpoints and streaming channels.",
  },
  {
    q: "Can I use this in production agents?",
    a: "Yes. /v1/context and /v1/ask are optimized for prompt-injection workflows and low-latency agent use.",
  },
  {
    q: "Are webhook payloads signed?",
    a: "Yes. Webhook events are signed with HMAC-SHA256 so receivers can verify authenticity.",
  },
]

export default function FeaturesPage() {
  const faqJsonLd = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: FAQ.map((item) => ({
      "@type": "Question",
      name: item.q,
      acceptedAnswer: {
        "@type": "Answer",
        text: item.a,
      },
    })),
  }

  return (
    <main style={{ paddingTop: "48px", paddingBottom: "80px", backgroundColor: "var(--bg)" }}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(faqJsonLd) }}
      />

      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <header style={{ textAlign: "center", marginBottom: "40px" }}>
          <span className="font-orbitron text-xs tracking-widest" style={{ color: "var(--dim)" }}>
            [ API FEATURES ]
          </span>
          <h1 className="font-outfit text-3xl font-bold" style={{ color: "var(--ink)", marginTop: "12px" }}>
            Features
          </h1>
          <p className="font-outfit text-base" style={{ color: "var(--muted)", maxWidth: "700px", margin: "12px auto 0" }}>
            Everything in one reconciled API: model catalogue, history, recommendations,
            natural-language query endpoints, stream updates, and webhook automation.
          </p>
        </header>

        <section className="grid gap-4 md:grid-cols-3" style={{ marginBottom: "48px" }}>
          {FEATURE_GROUPS.map((group) => (
            <article
              key={group.title}
              style={{
                background: "var(--surface)",
                border: "1px solid var(--borderDk)",
                padding: "20px",
              }}
            >
              <h2 className="font-outfit text-lg font-semibold" style={{ color: "var(--ink)", marginBottom: "12px" }}>
                {group.title}
              </h2>
              <ul className="space-y-2">
                {group.items.map((item) => (
                  <li key={item} className="font-outfit text-sm" style={{ color: "var(--muted)" }}>
                    • {item}
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </section>

        <section aria-labelledby="faq-heading" style={{ position: "relative" }}>
          <div
            aria-hidden
            style={{
              position: "absolute",
              inset: "-18px -12px auto -12px",
              height: "140px",
              background:
                "radial-gradient(70% 120% at 50% 0%, color-mix(in srgb, var(--accent) 22%, transparent) 0%, transparent 72%)",
              pointerEvents: "none",
            }}
          />

          <header style={{ marginBottom: "18px", position: "relative" }}>
            <h2 id="faq-heading" className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)", marginBottom: "8px" }}>
              FAQ
            </h2>
            <p className="font-outfit text-sm" style={{ color: "var(--dim)" }}>
              Quick answers to common implementation questions.
            </p>
          </header>

          <div className="space-y-3" style={{ position: "relative" }}>
            {FAQ.map((item, idx) => (
              <details
                key={item.q}
                style={{
                  border: "1px solid color-mix(in srgb, var(--border) 75%, var(--accent) 25%)",
                  background:
                    "linear-gradient(180deg, color-mix(in srgb, var(--surface) 92%, var(--accent) 8%) 0%, var(--surface) 100%)",
                  boxShadow: "0 1px 0 rgba(255,255,255,0.03) inset, 0 12px 32px rgba(0,0,0,0.12)",
                  overflow: "hidden",
                }}
              >
                <summary
                  className="font-outfit"
                  style={{
                    listStyle: "none",
                    cursor: "pointer",
                    padding: "14px 16px",
                    color: "var(--ink)",
                    fontSize: "15px",
                    fontWeight: 600,
                    display: "flex",
                    alignItems: "center",
                    gap: "10px",
                  }}
                >
                  <span
                    aria-hidden
                    style={{
                      width: "22px",
                      height: "22px",
                      borderRadius: "999px",
                      border: "1px solid color-mix(in srgb, var(--accent) 45%, var(--border) 55%)",
                      color: "var(--accent)",
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      fontSize: "12px",
                      lineHeight: 1,
                      flexShrink: 0,
                    }}
                  >
                    {idx + 1}
                  </span>
                  <span>{item.q}</span>
                </summary>

                <div
                  style={{
                    padding: "0 16px 15px 48px",
                    borderTop: "1px solid color-mix(in srgb, var(--border) 82%, var(--accent) 18%)",
                  }}
                >
                  <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "10px", lineHeight: 1.6 }}>
                    {item.a}
                  </p>
                </div>
              </details>
            ))}
          </div>
        </section>
      </div>
    </main>
  )
}
