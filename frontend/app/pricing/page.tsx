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

type HttpMethod = "GET" | "POST" | "DELETE"

interface Endpoint {
  method: HttpMethod
  path: string
  desc: string
}

const METHOD_STYLES: Record<HttpMethod, { bg: string; color: string }> = {
  GET:    { bg: "rgba(16,126,114,0.1)",  color: "var(--accentDk)" },
  POST:   { bg: "rgba(5,150,105,0.1)",   color: "var(--green)" },
  DELETE: { bg: "rgba(220,38,38,0.1)",   color: "var(--red)" },
}

const FEATURE_GROUPS: { title: string; accent: string; endpoints: Endpoint[] }[] = [
  {
    title: "Core Pricing API",
    accent: "var(--accent)",
    endpoints: [
      { method: "GET",  path: "/v1/models",        desc: "Full model catalogue" },
      { method: "GET",  path: "/v1/models/:id",    desc: "Model detail" },
      { method: "GET",  path: "/v1/providers",     desc: "Provider catalogue" },
      { method: "GET",  path: "/v1/compare",       desc: "Side-by-side model comparison" },
      { method: "GET",  path: "/v1/changes",       desc: "Recent price deltas" },
    ],
  },
  {
    title: "History & Context",
    accent: "#8B5CF6",
    endpoints: [
      { method: "GET", path: "/v1/models/:id/history", desc: "Full price timeline" },
      { method: "GET", path: "/v1/context",             desc: "Context snapshot for system prompts" },
      { method: "GET", path: "/v1/recommend",           desc: "Model recommendations by task/price" },
    ],
  },
  {
    title: "Agent & Automation",
    accent: "#F59E0B",
    endpoints: [
      { method: "POST",   path: "/v1/ask",           desc: "Natural language pricing queries" },
      { method: "GET",    path: "/v1/stream/changes", desc: "SSE stream with replay semantics" },
      { method: "POST",   path: "/v1/webhooks",       desc: "Signed webhook subscriptions" },
      { method: "DELETE", path: "/v1/webhooks/:id",   desc: "Webhook removal" },
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
      acceptedAnswer: { "@type": "Answer", text: item.a },
    })),
  }

  return (
    <main style={{ paddingTop: "48px", paddingBottom: "80px", backgroundColor: "var(--bg)" }}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(faqJsonLd) }}
      />

      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">

        {/* Header */}
        <header style={{ textAlign: "center", marginBottom: "48px" }}>
          <span className="font-orbitron text-xs tracking-widest" style={{ color: "var(--dim)" }}>
            [ API FEATURES ]
          </span>
          <h1 className="font-outfit text-3xl font-bold" style={{ color: "var(--ink)", marginTop: "12px" }}>
            Features
          </h1>
          <p className="font-outfit text-base" style={{ color: "var(--muted)", maxWidth: "640px", margin: "12px auto 0", lineHeight: 1.7 }}>
            Everything in one reconciled API: model catalogue, history, recommendations,
            natural-language query endpoints, stream updates, and webhook automation.
          </p>
        </header>

        {/* Feature cards */}
        <section className="grid gap-4 md:grid-cols-3" style={{ marginBottom: "56px" }}>
          {FEATURE_GROUPS.map((group) => (
            <article
              key={group.title}
              style={{
                border: "1px solid var(--border)",
                borderTop: `3px solid ${group.accent}`,
                padding: "20px",
                backgroundColor: "var(--surface)",
              }}
            >
              <h2 className="font-outfit text-base font-semibold" style={{ color: "var(--ink)", marginBottom: "16px" }}>
                {group.title}
              </h2>
              <ul style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
                {group.endpoints.map((ep) => {
                  const ms = METHOD_STYLES[ep.method]
                  return (
                    <li key={ep.path} style={{ display: "flex", flexDirection: "column", gap: "3px" }}>
                      <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                        <span
                          className="font-orbitron"
                          style={{
                            fontSize: "0.6rem",
                            letterSpacing: "0.05em",
                            padding: "1px 5px",
                            backgroundColor: ms.bg,
                            color: ms.color,
                            flexShrink: 0,
                          }}
                        >
                          {ep.method}
                        </span>
                        <span
                          className="font-orbitron text-xs"
                          style={{ color: "var(--ink)", letterSpacing: "0.02em" }}
                        >
                          {ep.path}
                        </span>
                      </div>
                      <span
                        className="font-outfit text-xs"
                        style={{ color: "var(--muted)", paddingLeft: "2px" }}
                      >
                        {ep.desc}
                      </span>
                    </li>
                  )
                })}
              </ul>
            </article>
          ))}
        </section>

        {/* Divider */}
        <div
          style={{
            borderTop: "1px solid var(--border)",
            marginBottom: "40px",
            width: "min(100vw, 80rem)",
            position: "relative",
            left: "50%",
            transform: "translateX(-50%)",
          }}
        />

        {/* FAQ */}
        <section aria-labelledby="faq-heading">
          <div className="mx-auto max-w-4xl px-6 py-8 sm:px-10">
            <span
              className="font-orbitron text-xs tracking-widest"
              style={{ color: "var(--dim)", display: "block", textAlign: "center", marginBottom: "8px" }}
            >
              [ FAQ ]
            </span>
            <h2
              id="faq-heading"
              className="font-outfit text-3xl font-bold text-center"
              style={{ color: "var(--ink)", marginBottom: "28px" }}
            >
              Frequently Asked
            </h2>

            <div style={{ border: "1px solid var(--border)" }}>
              {FAQ.map((item, i) => (
                <details
                  key={item.q}
                  className="group"
                  style={{
                    borderBottom: i < FAQ.length - 1 ? "1px solid var(--border)" : "none",
                  }}
                >
                  <summary
                    className="list-none cursor-pointer select-none py-4 px-5 font-outfit text-base font-medium flex items-start gap-4"
                    style={{ color: "var(--ink)" }}
                  >
                    <span
                      className="mt-1 flex h-5 w-5 shrink-0 items-center justify-center font-orbitron text-xs"
                      style={{ color: "var(--accent)" }}
                    >
                      <span className="group-open:hidden">+</span>
                      <span className="hidden group-open:inline">−</span>
                    </span>
                    <span>{item.q}</span>
                  </summary>
                  <p
                    className="pb-5 px-5 pl-14 font-outfit text-base leading-relaxed"
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
