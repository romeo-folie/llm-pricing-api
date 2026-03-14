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
  GET:    { bg: "var(--accentLt)",          color: "var(--accentDk)" },
  POST:   { bg: "rgba(5,150,105,0.12)",     color: "var(--green)" },
  DELETE: { bg: "rgba(220,38,38,0.1)",      color: "var(--red)" },
}

const FEATURE_GROUPS: { title: string; tag: string; endpoints: Endpoint[] }[] = [
  {
    title: "Core Pricing API",
    tag: "PRICING",
    endpoints: [
      { method: "GET", path: "/v1/models",        desc: "Full model catalogue with current prices and metadata" },
      { method: "GET", path: "/v1/models/:id",    desc: "Model detail, full price history, and source attribution" },
      { method: "GET", path: "/v1/providers",     desc: "Provider catalogue" },
      { method: "GET", path: "/v1/compare",       desc: "Side-by-side model comparison" },
      { method: "GET", path: "/v1/changes",       desc: "Recent price deltas with source and confidence" },
    ],
  },
  {
    title: "History & Context",
    tag: "HISTORY",
    endpoints: [
      { method: "GET", path: "/v1/models/:id/history", desc: "Full price timeline — every change, every source" },
      { method: "GET", path: "/v1/context",             desc: "~2k token pricing snapshot for agent system prompts" },
      { method: "GET", path: "/v1/recommend",           desc: "Ranked model recommendations by task, context, and price" },
    ],
  },
  {
    title: "Agent & Automation",
    tag: "AGENT",
    endpoints: [
      { method: "POST",   path: "/v1/ask",           desc: "Natural language + structured pricing response" },
      { method: "GET",    path: "/v1/stream/changes", desc: "SSE stream with Last-Event-ID reconnection semantics" },
      { method: "POST",   path: "/v1/webhooks",       desc: "Signed webhook subscriptions for price change events" },
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

      <div className="mx-auto max-w-5xl px-4 sm:px-6 lg:px-8">

        {/* Header */}
        <header style={{ textAlign: "center", marginBottom: "48px" }}>
          <span className="font-orbitron text-xs tracking-widest" style={{ color: "var(--dim)" }}>
            [ API FEATURES ]
          </span>
          <h1 className="font-outfit text-3xl font-bold" style={{ color: "var(--ink)", marginTop: "12px" }}>
            Features
          </h1>
          <p className="font-outfit text-base" style={{ color: "var(--muted)", maxWidth: "600px", margin: "12px auto 0", lineHeight: 1.7 }}>
            Everything in one reconciled API: model catalogue, history, recommendations,
            natural-language query endpoints, stream updates, and webhook automation.
          </p>
        </header>

        {/* Unified endpoint reference table */}
        <section style={{ marginBottom: "56px" }}>
          <table
            className="w-full"
            style={{ border: "1px dashed var(--borderDk)", borderSpacing: 0, borderCollapse: "collapse" }}
          >
            <caption className="sr-only">LLMRates API endpoint reference</caption>
            <thead className="sr-only">
              <tr>
                <th scope="col">Method</th>
                <th scope="col">Path</th>
                <th scope="col">Description</th>
              </tr>
            </thead>
            <tbody>
              {FEATURE_GROUPS.map((group) => (
                <>
                  {/* Group header row */}
                  <tr key={`header-${group.title}`}>
                    <td
                      colSpan={3}
                      style={{
                        padding: "10px 20px",
                        backgroundColor: "var(--surfaceLo)",
                        borderTop: "1px dashed var(--border)",
                        borderBottom: "1px dashed var(--border)",
                      }}
                    >
                      <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
                        <span
                          className="font-orbitron text-xs"
                          style={{ color: "var(--dim)", letterSpacing: "0.08em" }}
                        >
                          {group.tag}
                        </span>
                        <span
                          className="font-outfit text-sm font-semibold"
                          style={{ color: "var(--ink)" }}
                        >
                          {group.title}
                        </span>
                      </div>
                    </td>
                  </tr>

                  {/* Endpoint rows */}
                  {group.endpoints.map((ep) => {
                    const ms = METHOD_STYLES[ep.method]
                    return (
                      <tr
                        key={ep.path}
                        style={{ borderTop: "1px dashed var(--border)" }}
                      >
                        <td style={{ padding: "12px 20px", width: "1%", whiteSpace: "nowrap" }}>
                          <span
                            className="font-orbitron text-xs font-semibold"
                            style={{
                              padding: "2px 6px",
                              backgroundColor: ms.bg,
                              color: ms.color,
                            }}
                          >
                            {ep.method}
                          </span>
                        </td>
                        <td style={{ padding: "12px 16px 12px 0", width: "1%", whiteSpace: "nowrap" }}>
                          <code
                            className="font-orbitron text-sm"
                            style={{ color: "var(--ink)" }}
                          >
                            {ep.path}
                          </code>
                        </td>
                        <td style={{ padding: "12px 20px 12px 0" }}>
                          <span
                            className="font-outfit text-sm"
                            style={{ color: "var(--muted)" }}
                          >
                            {ep.desc}
                          </span>
                        </td>
                      </tr>
                    )
                  })}
                </>
              ))}
            </tbody>
          </table>
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
