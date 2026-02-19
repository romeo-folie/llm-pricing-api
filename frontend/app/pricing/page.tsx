import type { Metadata } from "next"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"

export const revalidate = false

export const metadata: Metadata = {
  title: "LLM Pricing API — Plans & Pricing | LLMPrice",
  description:
    "Transparent, reconciled LLM token pricing. Free tier, Developer API access, and Pro with webhooks and SLA.",
  openGraph: {
    title: "LLM Pricing API Plans",
    description:
      "Free to $50/mo. Developer API: $15/mo, 10k req/day. Pro: $50/mo, unlimited + webhooks.",
    type: "website",
  },
}

interface Feature {
  label: string
  included: boolean
  agent?: boolean
}

interface Tier {
  rank:     string
  name:     string
  price:    string
  requests: string
  ctaLabel: string
  ctaHref:  string
  featured: boolean
  features: Feature[]
}

const TIERS: Tier[] = [
  {
    rank:     "RECRUIT",
    name:     "Free",
    price:    "$0",
    requests: "100 req/day",
    ctaLabel: "Browse Models",
    ctaHref:  "/models",
    featured: false,
    features: [
      { label: "GET /v1/models — list all models",         included: true  },
      { label: "GET /v1/models/:id — model detail",        included: true  },
      { label: "GET /v1/compare — up to 5 models",        included: true  },
      { label: "GET /v1/providers — provider list",        included: true  },
      { label: "GET /v1/changes — recent price changes",   included: true  },
      { label: "Price history (Dev+)",                     included: false },
      { label: "⚡ Agent context snapshot (Dev+)",         included: false, agent: true },
      { label: "⚡ Natural language queries (Dev+)",       included: false, agent: true },
      { label: "⚡ SSE change stream (Dev+)",              included: false, agent: true },
      { label: "Webhooks (Pro only)",                      included: false },
      { label: "SLA guarantee (Pro only)",                 included: false },
    ],
  },
  {
    rank:     "ENGINEER",
    name:     "Developer",
    price:    "$15",
    requests: "10,000 req/day",
    ctaLabel: "Get API Key",
    ctaHref:  process.env.NEXT_PUBLIC_LS_CHECKOUT_DEV || "/pricing",
    featured: true,
    features: [
      { label: "Everything in Free",                               included: true  },
      { label: "GET /v1/models/:id/history — price history",      included: true  },
      { label: "⚡ GET /v1/context — agent context snapshot",     included: true,  agent: true },
      { label: "⚡ POST /v1/ask — natural language queries",      included: true,  agent: true },
      { label: "⚡ GET /v1/stream/changes — SSE stream",         included: true,  agent: true },
      { label: "⚡ GET /v1/recommend — model recommendations",   included: true,  agent: true },
      { label: "Webhooks (Pro only)",                              included: false },
      { label: "SLA guarantee (Pro only)",                         included: false },
    ],
  },
  {
    rank:     "ARCHITECT",
    name:     "Pro",
    price:    "$50",
    requests: "Unlimited",
    ctaLabel: "Go Pro",
    ctaHref:  process.env.NEXT_PUBLIC_LS_CHECKOUT_PRO || "/pricing",
    featured: false,
    features: [
      { label: "Everything in Developer",                         included: true  },
      { label: "POST /v1/webhooks — webhook registration",        included: true  },
      { label: "DELETE /v1/webhooks/:id",                         included: true  },
      { label: "Unlimited requests",                              included: true  },
      { label: "4hr SLA during business hours",                   included: true  },
      { label: "Priority support",                                 included: true  },
    ],
  },
]

const FAQ = [
  {
    q: "How are prices kept up to date?",
    a: "We scrape OpenRouter every 6 hours and LiteLLM and provider documentation daily. Our reconciliation engine requires agreement from at least 2 sources before publishing any price change.",
  },
  {
    q: "What does 'reconciled pricing' mean?",
    a: "Our diff engine flags price discrepancies greater than 5% between sources for human review. Only values confirmed by multiple sources are published to the API — no raw, unverified scrape data.",
  },
  {
    q: "Do I need a credit card for the Free tier?",
    a: "No. The Free tier is genuinely free with no payment information required. You can start making API calls immediately after generating a key.",
  },
  {
    q: "What happens if I exceed my rate limit?",
    a: "You'll receive a 429 response with a Retry-After header indicating when you can make your next request. There are no overage charges — the Free and Developer tiers are hard-capped.",
  },
  {
    q: "Can I use this in my AI agent's system prompt?",
    a: "Yes. GET /v1/context returns a ~2,000 token pricing snapshot specifically designed for injection into agent system prompts. It includes current prices, confidence scores, and source attribution. Available on Developer tier.",
  },
  {
    q: "Is there a webhook for price changes?",
    a: "Yes, on the Pro tier. Register your HTTPS endpoint via POST /v1/webhooks. Payloads are signed with HMAC-SHA256, delivered at-least-once with 3 retries over 15 minutes.",
  },
]

export default function PricingPage() {
  return (
    <main style={{ paddingTop: "48px", paddingBottom: "80px", backgroundColor: "var(--bg)" }}>
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">

        {/* Header */}
        <div style={{ textAlign: "center", marginBottom: "48px" }}>
          <span
            className="font-orbitron text-xs tracking-widest"
            style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
          >
            [ PLANS &amp; PRICING ]
          </span>
          <h1
            className="font-outfit text-3xl font-bold"
            style={{ color: "var(--ink)", marginBottom: "12px" }}
          >
            Plans & Pricing
          </h1>
          <p className="font-outfit text-base" style={{ color: "var(--muted)", maxWidth: "480px", margin: "0 auto" }}>
            Reconciled LLM pricing data. From free exploration to agent-grade access with full history and webhooks.
          </p>
        </div>

        {/* Rank progression strip */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: "12px",
            marginBottom: "36px",
          }}
        >
          {["RECRUIT", "ENGINEER", "ARCHITECT"].map((rank, i) => (
            <div key={rank} style={{ display: "flex", alignItems: "center", gap: "12px" }}>
              <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                <span
                  className="font-orbitron text-xs"
                  style={{
                    color: i === 1 ? "var(--accent)" : "var(--dim)",
                    letterSpacing: "0.08em",
                  }}
                >
                  {["▪", "▪▪", "▪▪▪"][i]}
                </span>
                <span
                  className="font-orbitron text-xs"
                  style={{ color: i === 1 ? "var(--accent)" : "var(--dim)", letterSpacing: "0.06em" }}
                >
                  {rank}
                </span>
              </div>
              {i < 2 && (
                <span className="font-outfit text-sm" style={{ color: "var(--border)" }}>──→</span>
              )}
            </div>
          ))}
        </div>

        {/* Tier cards */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))",
            gap: "16px",
            marginBottom: "64px",
          }}
        >
          {TIERS.map((tier) => (
            <div
              key={tier.name}
              style={{
                position: "relative",
                padding: "24px",
                backgroundColor: "var(--surface)",
                border: "1px solid var(--borderDk)",
                borderTop: "1px solid var(--borderDk)",
              }}
            >
              {tier.featured && (
                <div
                  className="font-orbitron text-xs"
                  style={{
                    position: "absolute",
                    top: "12px",
                    right: "12px",
                    padding: "2px 8px",
                    border: "1px solid var(--accent)",
                    color: "var(--accent)",
                    backgroundColor: "var(--accentLt)",
                    letterSpacing: "0.06em",
                  }}
                >
                  [ POPULAR ]
                </div>
              )}

              {/* Rank badge */}
              <div
                className="font-orbitron text-xs"
                style={{
                  marginBottom: "8px",
                  color: tier.featured ? "var(--accent)" : tier.name === "Pro" ? "var(--ink)" : "var(--dim)",
                  letterSpacing: "0.1em",
                }}
              >
                [ {tier.rank} ]
              </div>

              {/* Tier name */}
              <h2
                className="font-outfit text-xl font-bold"
                style={{ color: "var(--ink)", marginBottom: "4px" }}
              >
                {tier.name}
              </h2>

              {/* Price */}
              <div style={{ display: "flex", alignItems: "baseline", gap: "4px", marginBottom: "4px" }}>
                <span className="font-orbitron text-3xl" style={{ color: "var(--ink)" }}>
                  {tier.price}
                </span>
                <span className="font-outfit text-sm" style={{ color: "var(--muted)" }}>/mo</span>
              </div>

              <div
                className="font-outfit text-xs"
                style={{ color: "var(--dim)", marginBottom: "20px" }}
              >
                {tier.requests}
              </div>

              {/* CTA */}
              <a
                href={tier.ctaHref}
                className="font-outfit text-sm"
                style={{
                  display: "block",
                  width: "100%",
                  padding: "10px",
                  textAlign: "center",
                  border:           tier.featured ? "none" : "1px solid var(--border)",
                  backgroundColor:  tier.featured ? "var(--accent)" : "var(--surfaceLo)",
                  color:            tier.featured ? "white" : "var(--text)",
                  textDecoration:   "none",
                  fontWeight:       600,
                  marginBottom:     "20px",
                }}
              >
                {tier.ctaLabel}
              </a>

              {/* Divider */}
              <div style={{ borderTop: "1px solid var(--border)", marginBottom: "16px" }} />

              {/* Features */}
              <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: "8px" }}>
                {tier.features.map((f) => (
                  <li
                    key={f.label}
                    className="font-outfit text-xs"
                    style={{
                      display: "flex",
                      alignItems: "flex-start",
                      gap: "6px",
                      color: f.included ? "var(--text)" : "var(--dim)",
                    }}
                  >
                    <span style={{ flexShrink: 0, color: f.included ? "var(--green)" : "var(--dim)" }}>
                      {f.included ? "✓" : "✗"}
                    </span>
                    {f.label}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        {/* FAQ */}
        <div style={{ borderTop: "1px solid var(--border)", paddingTop: "48px" }}>
          <div style={{ maxWidth: "720px" }}>
            <h2
              className="font-outfit text-lg font-bold"
              style={{ color: "var(--ink)", marginBottom: "16px" }}
            >
              FAQ
            </h2>
            <Accordion type="single" collapsible>
              {FAQ.map((item, i) => (
                <AccordionItem key={i} value={`faq-${i}`}>
                  <AccordionTrigger className="font-outfit text-sm" style={{ color: "var(--ink)" }}>
                    {item.q}
                  </AccordionTrigger>
                  <AccordionContent className="font-outfit text-sm" style={{ color: "var(--muted)" }}>
                    {item.a}
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        </div>

      </div>
    </main>
  )
}
