import type { Metadata } from "next"
import { ArrowUpRight } from "lucide-react"
import HeroScene from "@/components/hero/HeroScene"
import { PricingTicker } from "@/components/hero/PricingTicker"
import { FeatureSections } from "@/components/features"
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
  },
  {
    method: "POST",
    path: "/v1/ask",
    desc: "Natural language → structured pricing response",
  },
  {
    method: "GET",
    path: "/v1/stream/changes",
    desc: "SSE stream with Last-Event-ID reconnection",
  },
  {
    method: "GET",
    path: "/v1/recommend",
    desc: "Ranked model recommendations by task, context, and price",
  },
]

const FEATURE_HIGHLIGHTS = [
  {
    title: "Model catalogue",
    points: ["Model list + detail", "Provider directory", "Compare up to 5 models", "Recent changes"],
  },
  {
    title: "Agent workflows",
    points: ["Price history", "Agent APIs (/context, /ask, SSE)", "Recommendations", "Webhook automation"],
  },
  {
    title: "Reliability",
    points: ["Source attribution", "Reconciliation confidence", "Replay-safe stream", "Signed webhooks"],
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
  { name: "OpenAI",       logo: "/provider-logos/openai.svg",       logoDark: "/provider-logos/openai-color.svg" },
  { name: "Anthropic",    logo: "/provider-logos/anthropic.svg",    logoDark: "/provider-logos/anthropic-color.svg" },
  { name: "Google",       logo: "/provider-logos/google-color.svg" },
  { name: "OpenRouter",   logo: "/provider-logos/openrouter.svg",   logoDark: "/provider-logos/openrouter-color.svg" },
  { name: "LiteLLM",      logo: "/provider-logos/litellm.png" },
  { name: "Hugging Face", logo: "/provider-logos/huggingface.svg" },
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
          hour12: false,
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
                  View Features
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
                    style={{ color: "var(--ink)" }}
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
                    style={{ color: "var(--ink)" }}
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
                    style={{ color: "var(--ink)" }}
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
          <div className="flex flex-wrap items-start justify-center gap-x-10 gap-y-4">
            {DATA_SOURCES.map((source) => (
              <div key={source.name} className="flex flex-col items-center gap-1.5">
                {source.logoDark ? (
                  <picture>
                    <source srcSet={source.logoDark} media="(prefers-color-scheme: dark)" />
                    <img
                      src={source.logo}
                      alt={source.name}
                      title={source.name}
                      className="h-6 w-auto"
                    />
                  </picture>
                ) : (
                  <img
                    src={source.logo}
                    alt={source.name}
                    title={source.name}
                    className="h-6 w-auto"
                  />
                )}
                <span className="font-outfit text-xs font-medium" style={{ color: "var(--muted)" }}>
                  {source.name}
                </span>
              </div>
            ))}
          </div>
        </div>
        {/* Live pricing ticker — full-bleed inside bordered container */}
        <PricingTicker />
        </div>
      </section>

      {/* ── Features ─────────────────────────────────────────────────────── */}
      <section aria-labelledby="features-heading">
        <div className="max-w-7xl mx-auto" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="max-w-6xl mx-auto px-6 py-16">
          <div className="mb-10">
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
                Explore Features
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
                    
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
        </div>
      </section>

      {/* ── Feature highlights ──────────────────────────────────────────── */}
      <section aria-labelledby="feature-highlights-heading" style={{ borderBottom: "1px solid var(--border)" }}>
        <FeatureSections />
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
            Start exploring models immediately. Use the full API surface for history, recommendations, stream updates, and webhook automation.
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
              Browse Models
            </a>
            <a
              href="/pricing"
              className="font-outfit text-sm font-semibold px-8 py-3"
              style={{
                color: "var(--text)",
                border: "1px solid var(--border)",
              }}
            >
              Explore Features
            </a>
          </div>
        </div>
      </section>
    </main>
  )
}
