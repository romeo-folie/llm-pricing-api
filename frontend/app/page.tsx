import type { Metadata } from "next"
import HeroScene from "@/components/hero/HeroScene"
import { getModels, getProviders, getChanges } from "@/lib/api"

// ─── Metadata ─────────────────────────────────────────────────────────────────

export const metadata: Metadata = {
  title: "LLMPrice — Reconciled LLM Token Pricing for Agents & Developers",
  description:
    "Multi-source reconciled LLM token pricing with full price history, real-time change feed, and agent-optimized APIs. Compare models, calculate costs, and track price changes.",
  openGraph: {
    title: "LLMPrice — Reconciled LLM Token Pricing",
    description:
      "Free and paid tiers. Full price history. /v1/context, /v1/ask, SSE stream. Built for agents and developers.",
    type: "website",
    images: [{ url: "/hero.webp", width: 1200, height: 800, alt: "LLMPrice hero scene" }],
  },
}

// ─── Static data ──────────────────────────────────────────────────────────────

const FEATURES = [
  {
    icon: "◈",
    title: "Full Price History",
    description:
      "Every model, every change — timestamped and immutable. Not just a snapshot, the complete timeline.",
    accent: false,
  },
  {
    icon: "⬡",
    title: "Multi-Source Reconciliation",
    description:
      "OpenRouter + LiteLLM + provider docs. 2-source agreement required before publishing. Flagged discrepancies logged.",
    accent: false,
  },
  {
    icon: "⚡",
    title: "Agent-Optimized APIs",
    description:
      "/v1/context for clean system prompts, /v1/ask for NL queries, SSE stream for live deltas.",
    accent: true,
  },
  {
    icon: "◉",
    title: "Real-Time Change Feed",
    description:
      "Price changes surfaced within 60 seconds. Trust metadata — confidence, source, age — on every record.",
    accent: false,
  },
]

const AGENT_ENDPOINTS = [
  {
    method: "GET",
    path: "/v1/context",
    desc: "~2k token pricing snapshot for agent system prompts",
    tier: "Dev+",
  },
  {
    method: "POST",
    path: "/v1/ask",
    desc: "Natural language → structured pricing response",
    tier: "Dev+",
  },
  {
    method: "GET",
    path: "/v1/stream/changes",
    desc: "SSE stream with Last-Event-ID reconnection",
    tier: "Dev+",
  },
  {
    method: "GET",
    path: "/v1/recommend",
    desc: "Ranked model recommendations by task, context, and price",
    tier: "Dev+",
  },
]

const TIER_SUMMARY = [
  {
    rank: "RECRUIT",
    name: "Free",
    price: "$0",
    period: "/mo",
    requests: "100 req/day",
    highlight: false,
    cta: "Browse Models",
    href: "/models",
    features: ["Model list + detail", "Provider list", "Compare up to 5", "Recent changes"],
  },
  {
    rank: "ENGINEER",
    name: "Developer",
    price: "$15",
    period: "/mo",
    requests: "10,000 req/day",
    highlight: true,
    cta: "Get API Key",
    href: process.env.NEXT_PUBLIC_LS_CHECKOUT_DEV || "/pricing",
    features: ["Everything in Free", "Price history", "⚡ Agent APIs (/context, /ask, SSE)", "Model recommendations"],
  },
  {
    rank: "ARCHITECT",
    name: "Pro",
    price: "$50",
    period: "/mo",
    requests: "Unlimited",
    highlight: false,
    cta: "Go Pro",
    href: process.env.NEXT_PUBLIC_LS_CHECKOUT_PRO || "/pricing",
    features: ["Everything in Developer", "Webhooks", "99.9% SLA", "Priority support"],
  },
]

const TESTIMONIALS = [
  {
    quote:
      "We replaced three spreadsheets with this API. The price history alone is worth the subscription — no other source shows you the full timeline.",
    author: "@alex_dev",
    role: "AI infra engineer",
  },
  {
    quote:
      "The /v1/context endpoint is exactly what LLM agents need — a clean ≤2k token snapshot without hitting rate limits every call.",
    author: "@promptengineer",
    role: "Agent SDK author",
  },
  {
    quote:
      "Best pricing signal in the AI tooling space. The confidence metadata catches discrepancies before they hit production.",
    author: "@airesearcher",
    role: "ML researcher",
  },
]

// ─── SSR helpers ──────────────────────────────────────────────────────────────

async function fetchLandingStats() {
  const [modelsResult, providersResult, changesResult] = await Promise.allSettled([
    getModels(),
    getProviders(),
    getChanges(),
  ])

  const modelCount =
    modelsResult.status === "fulfilled" ? modelsResult.value.length : null
  const providerCount =
    providersResult.status === "fulfilled" ? providersResult.value.length : null
  const lastChange =
    changesResult.status === "fulfilled" && changesResult.value.length > 0
      ? changesResult.value[0]
      : null

  return { modelCount, providerCount, lastChange }
}

// ─── Page ──────────────────────────────────────────────────────────────────────

export default async function Home() {
  const { modelCount, providerCount, lastChange } = await fetchLandingStats()

  // Anchor to UTC so server and client produce identical strings (prevents hydration mismatch)
  const lastChangeLabel = lastChange
    ? new Intl.DateTimeFormat("en-US", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        timeZone: "UTC",
      }).format(new Date(lastChange.changed_at))
    : null

  return (
    <>
      {/* ── Hero ─────────────────────────────────────────────────────────── */}
      <section
        aria-labelledby="hero-heading"
        style={{
          borderBottom: "1px solid var(--border)",
          backgroundColor: "var(--surface)",
        }}
      >
        <div className="max-w-6xl mx-auto px-6 py-16 lg:py-24">
          <div className="grid lg:grid-cols-2 gap-12 items-center">
            {/* Left: copy */}
            <div className="flex flex-col gap-6">
              {/* eyebrow */}
              <div className="flex items-center gap-2">
                <span
                  className="font-orbitron text-xs font-semibold tracking-widest px-2 py-1 rounded-sm"
                  style={{
                    backgroundColor: "var(--accentLt)",
                    color: "var(--accentDk)",
                    border: "1px solid var(--accent)",
                  }}
                >
                  MULTI-SOURCE · RECONCILED
                </span>
              </div>

              <h1
                id="hero-heading"
                className="font-orbitron text-4xl lg:text-5xl font-extrabold leading-tight"
                style={{ color: "var(--ink)" }}
              >
                Reconciled LLM Pricing.{" "}
                <span style={{ color: "var(--accent)" }}>For Agents</span>{" "}
                &amp; Humans.
              </h1>

              <p
                className="font-outfit text-lg leading-relaxed"
                style={{ color: "var(--muted)" }}
              >
                Multi-source verified token pricing with full history, real-time change
                feed, and agent-native endpoints. Replace manual spreadsheets with a
                single trusted API.
              </p>

              {/* CTAs */}
              <div className="flex gap-3 flex-wrap">
                <a
                  href="/models"
                  className="font-outfit text-sm font-semibold px-6 py-3 rounded-sm"
                  style={{
                    backgroundColor: "var(--accent)",
                    color: "var(--surfaceHi)",
                    border: "1px solid var(--accentDk)",
                  }}
                >
                  Browse Models
                </a>
                <a
                  href="/pricing"
                  className="font-outfit text-sm font-semibold px-6 py-3 rounded-sm"
                  style={{
                    backgroundColor: "var(--surface)",
                    color: "var(--text)",
                    border: "1px solid var(--border)",
                  }}
                >
                  View Pricing
                </a>
              </div>

              {/* Stat strip */}
              <div
                className="flex flex-wrap gap-6 pt-2"
                style={{ borderTop: "1px solid var(--border)" }}
              >
                <div className="flex flex-col gap-0.5">
                  <span
                    className="font-orbitron text-2xl font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {modelCount !== null ? modelCount.toLocaleString() : "—"}
                  </span>
                  <span
                    className="font-outfit text-xs"
                    style={{ color: "var(--dim)" }}
                  >
                    models tracked
                  </span>
                </div>
                <div className="flex flex-col gap-0.5">
                  <span
                    className="font-orbitron text-2xl font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {providerCount !== null ? providerCount.toLocaleString() : "—"}
                  </span>
                  <span
                    className="font-outfit text-xs"
                    style={{ color: "var(--dim)" }}
                  >
                    providers
                  </span>
                </div>
                <div className="flex flex-col gap-0.5">
                  <span
                    className="font-orbitron text-lg font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {lastChangeLabel ?? "—"}
                  </span>
                  <span
                    className="font-outfit text-xs"
                    style={{ color: "var(--dim)" }}
                  >
                    last price change
                  </span>
                </div>
              </div>
            </div>

            {/* Right: hero scene — client component; poster WebP renders server-side via next/image */}
            <div className="w-full">
              <HeroScene style={{ minHeight: "360px" }} />
            </div>
          </div>
        </div>
      </section>

      {/* ── Features ─────────────────────────────────────────────────────── */}
      <section aria-labelledby="features-heading" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="mb-10">
            <h2
              id="features-heading"
              className="font-orbitron text-2xl font-bold mb-2"
              style={{ color: "var(--ink)" }}
            >
              What makes it different
            </h2>
            <p className="font-outfit text-base" style={{ color: "var(--muted)" }}>
              Most pricing tools give you a snapshot. We give you the full timeline with source attribution.
            </p>
          </div>

          <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {FEATURES.map((f) => (
              <div
                key={f.title}
                className="flex flex-col gap-3 p-5 rounded-sm"
                style={{
                  backgroundColor: f.accent ? "var(--accentLt)" : "var(--surface)",
                  border: f.accent
                    ? "1px solid var(--accent)"
                    : "1px solid var(--border)",
                  /* isometric depth treatment */
                  borderLeft: f.accent ? "3px solid var(--accent)" : "3px solid var(--borderDk)",
                }}
              >
                <span
                  className="font-orbitron text-2xl"
                  style={{ color: f.accent ? "var(--accent)" : "var(--accentDk)" }}
                  aria-hidden="true"
                >
                  {f.icon}
                </span>
                <h3
                  className="font-orbitron text-sm font-bold"
                  style={{ color: "var(--ink)" }}
                >
                  {f.title}
                </h3>
                <p
                  className="font-outfit text-sm leading-relaxed"
                  style={{ color: "var(--muted)" }}
                >
                  {f.description}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── Agent callout ────────────────────────────────────────────────── */}
      <section
        aria-labelledby="agent-heading"
        style={{
          borderBottom: "1px solid var(--border)",
          backgroundColor: "var(--surfaceLo)",
        }}
      >
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="flex flex-col lg:flex-row gap-12 items-start">
            {/* Left copy */}
            <div className="flex flex-col gap-4 lg:max-w-sm">
              <span
                className="font-orbitron text-xs font-semibold tracking-widest px-2 py-1 rounded-sm w-fit"
                style={{
                  backgroundColor: "var(--accentLt)",
                  color: "var(--accentDk)",
                  border: "1px solid var(--accent)",
                }}
              >
                ⚡ AGENT-OPTIMIZED
              </span>
              <h2
                id="agent-heading"
                className="font-orbitron text-2xl font-bold"
                style={{ color: "var(--ink)" }}
              >
                Built for LLM agents
              </h2>
              <p className="font-outfit text-base" style={{ color: "var(--muted)" }}>
                Dedicated endpoints designed to fit in agent system prompts, handle
                natural language queries, and stream price deltas in real time — without
                blowing your token budget.
              </p>
              <a
                href="/pricing"
                className="font-outfit text-sm font-semibold px-5 py-2.5 rounded-sm w-fit"
                style={{
                  backgroundColor: "var(--accent)",
                  color: "var(--surfaceHi)",
                  border: "1px solid var(--accentDk)",
                }}
              >
                Get Dev+ Access
              </a>
            </div>

            {/* Semantic endpoint table */}
            <table
              className="flex-1 rounded-sm overflow-hidden w-full"
              style={{ border: "1px solid var(--border)", borderCollapse: "collapse" }}
            >
              <caption className="sr-only">Agent-optimized API endpoints</caption>
              <thead className="sr-only">
                <tr>
                  <th scope="col">Method</th>
                  <th scope="col">Path</th>
                  <th scope="col">Description</th>
                  <th scope="col">Tier</th>
                </tr>
              </thead>
              <tbody>
                {AGENT_ENDPOINTS.map((ep, i) => (
                  <tr
                    key={ep.path}
                    style={{
                      borderTop: i === 0 ? "none" : "1px solid var(--border)",
                      backgroundColor: "var(--surface)",
                    }}
                  >
                    <td className="px-4 py-4 w-px whitespace-nowrap">
                      <span
                        className="font-orbitron text-xs font-semibold px-1.5 py-0.5 rounded-sm"
                        style={{
                          backgroundColor: "var(--accentLt)",
                          color: "var(--accentDk)",
                        }}
                      >
                        {ep.method}
                      </span>
                    </td>
                    <td className="px-4 py-4 w-px whitespace-nowrap">
                      <code
                        className="font-orbitron text-sm"
                        style={{ color: "var(--ink)" }}
                      >
                        {ep.path}
                      </code>
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className="font-outfit text-sm"
                        style={{ color: "var(--muted)" }}
                      >
                        {ep.desc}
                      </span>
                    </td>
                    <td className="px-4 py-4 w-px whitespace-nowrap">
                      <span
                        className="font-orbitron text-xs font-semibold px-1.5 py-0.5 rounded-sm"
                        style={{
                          backgroundColor: "var(--blueLt)",
                          color: "var(--blue)",
                        }}
                      >
                        {ep.tier}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* ── Pricing summary ──────────────────────────────────────────────── */}
      <section aria-labelledby="pricing-heading" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="mb-10 flex items-end justify-between flex-wrap gap-4">
            <div>
              <h2
                id="pricing-heading"
                className="font-orbitron text-2xl font-bold mb-2"
                style={{ color: "var(--ink)" }}
              >
                Simple pricing
              </h2>
              <p className="font-outfit text-base" style={{ color: "var(--muted)" }}>
                Start free. Upgrade when you need price history and agent endpoints.
              </p>
            </div>
            <a
              href="/pricing"
              className="font-outfit text-sm font-medium"
              style={{ color: "var(--accent)" }}
            >
              Full plan comparison →
            </a>
          </div>

          <div className="grid sm:grid-cols-3 gap-4">
            {TIER_SUMMARY.map((t) => (
              <div
                key={t.rank}
                className="flex flex-col gap-4 p-6 rounded-sm"
                style={{
                  backgroundColor: t.highlight ? "var(--surface)" : "var(--bg)",
                  border: t.highlight
                    ? "2px solid var(--accent)"
                    : "1px solid var(--border)",
                  borderLeft: t.highlight
                    ? "4px solid var(--accent)"
                    : "4px solid var(--borderDk)",
                  position: "relative",
                  // Extra top padding absorbs the POPULAR badge height so it never clips
                  paddingTop: t.highlight ? "2rem" : undefined,
                }}
              >
                {t.highlight && (
                  <span
                    className="absolute top-0 left-4 -translate-y-1/2 font-orbitron text-xs font-bold px-2 py-0.5 rounded-sm"
                    style={{
                      backgroundColor: "var(--accent)",
                      color: "var(--surfaceHi)",
                    }}
                  >
                    POPULAR
                  </span>
                )}

                <div>
                  <span
                    className="font-orbitron text-xs tracking-widest"
                    style={{ color: "var(--dim)" }}
                  >
                    {t.rank}
                  </span>
                  <h3
                    className="font-orbitron text-lg font-bold mt-0.5"
                    style={{ color: "var(--ink)" }}
                  >
                    {t.name}
                  </h3>
                </div>

                <div className="flex items-baseline gap-1">
                  <span
                    className="font-orbitron text-3xl font-extrabold"
                    style={{ color: t.highlight ? "var(--accent)" : "var(--ink)" }}
                  >
                    {t.price}
                  </span>
                  <span
                    className="font-outfit text-sm"
                    style={{ color: "var(--dim)" }}
                  >
                    {t.period}
                  </span>
                </div>

                <span
                  className="font-outfit text-xs"
                  style={{ color: "var(--muted)" }}
                >
                  {t.requests}
                </span>

                <ul className="flex flex-col gap-2">
                  {t.features.map((feat) => (
                    <li
                      key={feat}
                      className="font-outfit text-sm flex items-start gap-2"
                      style={{ color: "var(--text)" }}
                    >
                      <span style={{ color: "var(--green)" }} aria-hidden="true">✓</span>
                      {feat}
                    </li>
                  ))}
                </ul>

                <a
                  href={t.href}
                  className="font-outfit text-sm font-semibold px-4 py-2.5 rounded-sm text-center mt-auto"
                  style={{
                    backgroundColor: t.highlight ? "var(--accent)" : "var(--surface)",
                    color: t.highlight ? "var(--surfaceHi)" : "var(--text)",
                    border: t.highlight
                      ? "1px solid var(--accentDk)"
                      : "1px solid var(--border)",
                  }}
                >
                  {t.cta}
                </a>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── Testimonials ─────────────────────────────────────────────────── */}
      <section aria-labelledby="testimonials-heading" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <h2
            id="testimonials-heading"
            className="font-orbitron text-2xl font-bold mb-10"
            style={{ color: "var(--ink)" }}
          >
            What developers say
          </h2>
          <div className="grid sm:grid-cols-3 gap-4">
            {TESTIMONIALS.map((t) => (
              <figure
                key={t.author}
                className="flex flex-col gap-4 p-6 rounded-sm"
                style={{
                  backgroundColor: "var(--surface)",
                  border: "1px solid var(--border)",
                  borderLeft: "3px solid var(--borderDk)",
                }}
              >
                <blockquote>
                  <p
                    className="font-outfit text-base leading-relaxed"
                    style={{ color: "var(--text)" }}
                  >
                    &ldquo;{t.quote}&rdquo;
                  </p>
                </blockquote>
                <figcaption className="flex flex-col gap-0.5 mt-auto">
                  <span
                    className="font-orbitron text-xs font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {t.author}
                  </span>
                  <span
                    className="font-outfit text-xs"
                    style={{ color: "var(--dim)" }}
                  >
                    {t.role}
                  </span>
                </figcaption>
              </figure>
            ))}
          </div>
        </div>
      </section>

      {/* ── Final CTA ────────────────────────────────────────────────────── */}
      <section aria-labelledby="cta-heading" style={{ backgroundColor: "var(--surfaceLo)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16 text-center flex flex-col items-center gap-6">
          <h2
            id="cta-heading"
            className="font-orbitron text-3xl font-extrabold"
            style={{ color: "var(--ink)" }}
          >
            Start tracking LLM prices today
          </h2>
          <p
            className="font-outfit text-lg max-w-xl"
            style={{ color: "var(--muted)" }}
          >
            Free tier available. No credit card required. Full price history and
            agent APIs one tier away.
          </p>
          <div className="flex gap-3 flex-wrap justify-center">
            <a
              href="/models"
              className="font-outfit text-sm font-semibold px-8 py-3 rounded-sm"
              style={{
                backgroundColor: "var(--accent)",
                color: "var(--surfaceHi)",
                border: "1px solid var(--accentDk)",
              }}
            >
              Browse Models — Free
            </a>
            <a
              href="/pricing"
              className="font-outfit text-sm font-semibold px-8 py-3 rounded-sm"
              style={{
                backgroundColor: "var(--surface)",
                color: "var(--text)",
                border: "1px solid var(--border)",
              }}
            >
              Compare Plans
            </a>
          </div>
        </div>
      </section>
    </>
  )
}
