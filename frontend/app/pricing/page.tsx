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
                border: "1px solid var(--border)",
                padding: "20px",
              }}
            >
              <h2 className="font-outfit text-lg font-semibold" style={{ color: "var(--ink)", marginBottom: "12px" }}>
                {group.title}
              </h2>
              <ul className="space-y-2">
                {group.items.map((item) => (
                  <li
                    key={item}
                    className="font-outfit text-sm flex items-start gap-2"
                    style={{ color: "var(--muted)" }}
                  >
                    <span style={{ color: "var(--green)" }} aria-hidden="true">✓</span>
                    <span>{item}</span>
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </section>

        <div
          style={{
            borderTop: "1px solid var(--border)",
            marginTop: "8px",
            width: "min(100vw, 80rem)",
            position: "relative",
            left: "50%",
            transform: "translateX(-50%)",
          }}
        />

        <section aria-labelledby="faq-heading" style={{ marginTop: "32px" }}>
          <div className="mx-auto max-w-4xl px-6 py-8 sm:px-10">
            <h2
              id="faq-heading"
              className="font-outfit text-3xl font-bold text-center"
              style={{ color: "var(--ink)", marginBottom: "24px" }}
            >
              FAQ
            </h2>

            <div>
              {FAQ.map((item) => (
                <details
                  key={item.q}
                  className="group"
                >
                  <summary
                    className="list-none cursor-pointer select-none py-4 font-outfit text-base sm:text-lg font-medium tracking-tight flex items-start gap-4"
                    style={{ color: "var(--ink)" }}
                  >
                    <span
                      className="mt-1 flex h-5 w-5 shrink-0 items-center justify-center text-lg leading-none"
                      style={{ color: "var(--muted)" }}
                    >
                      <span className="group-open:hidden">+</span>
                      <span className="hidden group-open:inline">−</span>
                    </span>
                    <span>{item.q}</span>
                  </summary>
                  <p
                    className="pb-4 pl-9 font-outfit text-base leading-relaxed"
                    style={{ color: "var(--muted)", maxWidth: "80ch" }}
                  >
                    {item.a}
                  </p>
                </details>
              ))}
            </div>
          </div>
        </section>
      </div>
    </main>
  )
}
