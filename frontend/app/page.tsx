import type { Metadata } from "next"
import { ArrowUpRight } from "lucide-react"
import HeroScene from "@/components/hero/HeroScene"
import { getModels, getProviders, getChanges } from "@/lib/api"

// ─── Metadata ─────────────────────────────────────────────────────────────────

export const metadata: Metadata = {
  title: {
    absolute: "LLMRates — LLM Token Pricing Tracker | Compare GPT, Claude, Gemini Costs",
  },
  description:
    "Track and compare AI model pricing across OpenAI, Anthropic, Google, xAI, and Mistral. " +
    "Reconciled token costs with full price history, real-time change feed, and agent-optimized APIs. " +
    "See how much GPT-4o, Claude 3.5, Gemini, Grok cost per 1M tokens.",
  keywords:
    "llm pricing, ai model cost, gpt-4o pricing, claude 3.5 pricing, gemini pricing, grok pricing, " +
    "token cost comparison, ai api price tracker, openai pricing, anthropic pricing, " +
    "llm cost calculator, compare ai models, price per 1000 tokens",
  openGraph: {
    title: "LLMRates — Compare AI Model Pricing & Track Cost Changes",
    description:
      "Compare GPT-4o, Claude 3.5, Gemini, and Grok pricing side by side. Full price history, real-time changes, and agent-native APIs.",
    type: "website",
  },
}

// ─── Static data ──────────────────────────────────────────────────────────────

const FEATURES = [
  {
    icon: "◈",
    title: "Full Price History",
    description:
      "Every model, every change — timestamped and immutable. Not just a snapshot, the complete timeline.",
  },
  {
    icon: "⬡",
    title: "Multi-Source Reconciliation",
    description:
      "OpenRouter + LiteLLM + Hugging Face. 2-source agreement required before publishing. Flagged discrepancies logged.",
  },
  {
    icon: "⚡",
    title: "Agent-Optimized APIs",
    description:
      "/v1/context for clean system prompts, /v1/ask for NL queries, SSE stream for live deltas.",
  },
  {
    icon: "◉",
    title: "Real-Time Change Feed",
    description:
      "Price changes surfaced within 60 seconds. Trust metadata — confidence, source, age — on every record.",
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
    price: "$9.99",
    period: "/mo",
    requests: "10,000 req/day",
    highlight: true,
    cta: "Get API Key",
    href: process.env.NEXT_PUBLIC_LS_CHECKOUT_DEV || "/pricing",
    features: ["Everything in Free", "Price history", "Agent APIs (/context, /ask, SSE)", "Model recommendations"],
  },
  {
    rank: "ARCHITECT",
    name: "Pro",
    price: "$14.99",
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

const DATA_SOURCES = [
  "OpenRouter",
  "LiteLLM",
  "Hugging Face",
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

  const parsedDate = lastChange ? new Date(lastChange.changed_at) : null
  const lastChangeLabel =
    parsedDate && !isNaN(parsedDate.getTime())
      ? new Intl.DateTimeFormat("en-US", {
          month: "short",
          day: "numeric",
          hour: "2-digit",
          minute: "2-digit",
          timeZone: "UTC",
        }).format(parsedDate)
      : null

  return (
    <main>
      {/* ── Hero ─────────────────────────────────────────────────────────── */}
      <section aria-labelledby="hero-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-20 lg:py-32">
          <div className="grid lg:grid-cols-2 gap-12 items-center">
            {/* Left: copy */}
            <div className="flex flex-col gap-6">
              <h1
                id="hero-heading"
                className="font-outfit text-4xl lg:text-5xl font-extrabold leading-tight"
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

              {/* CTAs — sharp corners */}
              <div className="flex gap-3 flex-wrap">
                <a
                  href="/models"
                  className="font-outfit text-sm font-semibold px-6 py-3"
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
                  className="font-outfit text-sm font-semibold px-6 py-3"
                  style={{
                    color: "var(--text)",
                    border: "1px solid var(--border)",
                  }}
                >
                  View Pricing
                </a>
              </div>

              {/* Stat strip */}
              <div
                className="flex flex-wrap items-start gap-6 pt-4"
                style={{ borderTop: "1px solid var(--border)" }}
              >
                <div className="flex flex-col gap-0.5">
                  <span
                    className="font-orbitron text-2xl font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {(modelCount ?? 340).toLocaleString()}
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
                    {(providerCount ?? 7).toLocaleString()}
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
                    className="font-orbitron text-2xl font-bold"
                    style={{ color: "var(--accent)" }}
                  >
                    {lastChangeLabel ?? "<60s ago"}
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

            {/* Right: hero scene */}
            <div className="w-full">
              <HeroScene />
            </div>
          </div>
        </div>
        </div>
      </section>

      {/* ── Data Sources Bar ───────────────────────────────────────────── */}
      <section aria-label="Data sources">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-6">
          <div className="flex flex-wrap items-center justify-center gap-x-8 gap-y-3">
            {DATA_SOURCES.map((source) => (
              <span
                key={source}
                className="font-outfit text-sm font-medium"
                style={{ color: "var(--muted)" }}
              >
                {source}
              </span>
            ))}
          </div>
        </div>
        </div>
      </section>

      {/* ── Features ─────────────────────────────────────────────────────── */}
      <section aria-labelledby="features-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="mb-10">
            <span
              className="font-orbitron text-xs tracking-widest"
              style={{ color: "var(--dim)" }}
            >
              [ WHAT MAKES IT DIFFERENT ]
            </span>
            <h2
              id="features-heading"
              className="font-outfit text-2xl font-bold mt-3 mb-2"
              style={{ color: "var(--ink)" }}
            >
              Full history. Multiple sources. Zero guesswork.
            </h2>
            <p className="font-outfit text-base" style={{ color: "var(--muted)" }}>
              Most pricing tools give you a snapshot. We give you the full timeline with source attribution.
            </p>
          </div>

          <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {FEATURES.map((f) => (
              <div
                key={f.title}
                className="relative flex flex-col gap-3 p-5 transition-colors"
                style={{
                  border: "1px solid var(--border)",
                }}
              >
                {/* Arrow icon — top right */}
                <span
                  className="absolute top-3 right-3"
                  style={{ color: "var(--dim)" }}
                  aria-hidden="true"
                >
                  <ArrowUpRight size={16} />
                </span>

                <span
                  className="font-orbitron text-2xl"
                  style={{ color: "var(--accent)" }}
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
        </div>
      </section>

      {/* ── Agent callout ────────────────────────────────────────────────── */}
      <section aria-labelledby="agent-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="flex flex-col lg:flex-row gap-12 items-start">
            {/* Left copy */}
            <div className="flex flex-col gap-4 lg:max-w-sm">
              <span
                className="font-orbitron text-xs tracking-widest"
                style={{ color: "var(--dim)" }}
              >
                [ AGENT-OPTIMIZED ]
              </span>
              <h2
                id="agent-heading"
                className="font-outfit text-2xl font-bold"
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
                className="font-outfit text-sm font-semibold px-5 py-2.5 w-fit"
                style={{
                  backgroundColor: "var(--accent)",
                  color: "var(--surfaceHi)",
                  border: "1px solid var(--accentDk)",
                }}
              >
                Get Dev+ Access
              </a>
            </div>

            {/* Endpoint table — dashed border for architectural diagram feel */}
            <table
              className="flex-1 w-full"
              style={{ border: "1px dashed var(--borderDk)", borderSpacing: 0 }}
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
                    className="transition-colors"
                    style={{
                      borderTop: i === 0 ? "none" : "1px dashed var(--border)",
                    }}
                  >
                    <td className="px-4 py-4 w-px whitespace-nowrap">
                      <span
                        className="font-orbitron text-xs font-semibold px-1.5 py-0.5"
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
                        className="font-orbitron text-xs font-semibold px-1.5 py-0.5"
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
        </div>
      </section>

      {/* ── Pricing summary ──────────────────────────────────────────────── */}
      <section aria-labelledby="pricing-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="mb-10 flex items-end justify-between flex-wrap gap-4">
            <div>
              <span
                className="font-orbitron text-xs tracking-widest"
                style={{ color: "var(--dim)" }}
              >
                [ SIMPLE PRICING ]
              </span>
              <h2
                id="pricing-heading"
                className="font-outfit text-2xl font-bold mt-3 mb-2"
                style={{ color: "var(--ink)" }}
              >
                Start free. Upgrade when you need history.
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
                className="flex flex-col gap-4 p-6"
                style={{
                  border: "1px solid var(--border)",
                  position: "relative",
                  paddingTop: t.highlight ? "2rem" : undefined,
                }}
              >
                {t.highlight && (
                  <span
                    className="absolute top-0 left-4 -translate-y-1/2 font-orbitron text-xs font-bold px-2 py-0.5"
                    style={{
                      backgroundColor: "var(--accent)",
                      color: "var(--surfaceHi)",
                    }}
                  >
                    [ POPULAR ]
                  </span>
                )}

                <div>
                  <span
                    className="font-orbitron text-xs tracking-widest"
                    style={{ color: "var(--dim)" }}
                  >
                    [ {t.rank} ]
                  </span>
                  <h3
                    className="font-outfit text-lg font-bold mt-0.5"
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
                  className="font-outfit text-sm font-semibold px-4 py-2.5 text-center mt-auto"
                  style={{
                    backgroundColor: t.highlight ? "var(--accent)" : "transparent",
                    color: t.highlight ? "var(--surfaceHi)" : "var(--text)",
                    border: t.highlight ? "1px solid var(--accentDk)" : "1px solid var(--border)",
                  }}
                >
                  {t.cta}
                </a>
              </div>
            ))}
          </div>
        </div>
        </div>
      </section>

      {/* ── Testimonials ─────────────────────────────────────────────────── */}
      <section aria-labelledby="testimonials-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <span
            className="font-orbitron text-xs tracking-widest"
            style={{ color: "var(--dim)" }}
          >
            [ WHAT DEVELOPERS SAY ]
          </span>
          <h2
            id="testimonials-heading"
            className="font-outfit text-2xl font-bold mt-3 mb-10"
            style={{ color: "var(--ink)" }}
          >
            Trusted by AI engineers
          </h2>
          <div className="grid sm:grid-cols-3 gap-4">
            {TESTIMONIALS.map((t) => (
              <figure
                key={t.author}
                className="relative flex flex-col gap-4 p-6"
                style={{
                  border: "1px solid var(--border)",
                }}
              >
                {/* Decorative quotation mark */}
                <span
                  className="font-outfit text-4xl font-bold leading-none absolute top-4 right-5"
                  style={{ color: "var(--accentLt)" }}
                  aria-hidden="true"
                >
                  &ldquo;
                </span>

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
        </div>
      </section>

      {/* ── Final CTA ────────────────────────────────────────────────────── */}
      <section aria-labelledby="cta-heading">
        <div className="max-w-6xl mx-auto px-6 py-24 text-center flex flex-col items-center gap-6">
          <h2
            id="cta-heading"
            className="font-outfit text-3xl font-extrabold"
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
              className="font-outfit text-sm font-semibold px-8 py-3"
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
              className="font-outfit text-sm font-semibold px-8 py-3"
              style={{
                color: "var(--text)",
                border: "1px solid var(--border)",
              }}
            >
              Compare Plans
            </a>
          </div>
        </div>
      </section>
    </main>
  )
}
