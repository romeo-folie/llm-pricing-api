import type { Metadata } from "next"
import { notFound } from "next/navigation"
import { getModel } from "@/lib/api"
import { safeJsonLd } from "@/lib/utils"
import { formatPrice, formatContext, formatProviderName } from "@/lib/format"
import { decodeSlugParts } from "@/lib/routes"
import type { Model } from "@/lib/api"

export const dynamic = "force-dynamic"

interface PageProps {
  params: Promise<{ slug: string[] }>
}

// ─── Slug parsing ──────────────────────────────────────────────────────────────
// Splits "gpt-4o-vs-claude-3-5-sonnet" → ["gpt-4o", "claude-3-5-sonnet"].
// Non-greedy first group matches the FIRST -vs- occurrence.
// Returns null if either part contains a further "-vs-" — those slugs would
// resolve ambiguously and are excluded from sitemap generation anyway.

function parseSlugs(slug: string): [string, string] | null {
  const match = slug.match(/^(.+?)-vs-(.+)$/)
  if (!match) return null
  const [, a, b] = match
  if (a.includes("-vs-") || b.includes("-vs-")) return null
  return [a, b]
}

// ─── SEO keyword generation ───────────────────────────────────────────────────

function buildCompareSeo(a: Model, b: Model) {
  const title = `${a.name} vs ${b.name} — Token Cost Comparison`

  const description =
    `Compare ${a.name} vs ${b.name} pricing side by side. ` +
    `${a.name}: ${formatPrice(a.input_price_per_m)} input / ${formatPrice(a.output_price_per_m)} output per 1M tokens. ` +
    `${b.name}: ${formatPrice(b.input_price_per_m)} input / ${formatPrice(b.output_price_per_m)} output per 1M tokens. ` +
    `Context windows, confidence scores, and full price history included.`

  const keywords = [
    `${a.name} vs ${b.name}`,
    `${b.name} vs ${a.name}`,
    `compare ${a.name} ${b.name}`,
    `${a.name} vs ${b.name} pricing`,
    `${a.name} vs ${b.name} cost`,
    `${a.name} or ${b.name}`,
    `${a.name} ${b.name} comparison`,
    `${a.name} ${b.name} price difference`,
    `which is cheaper ${a.name} ${b.name}`,
    `${a.name} ${b.name} token cost`,
    `${a.provider} vs ${b.provider} llm pricing`,
  ].join(", ")

  return { title, description, keywords }
}

// ─── Metadata ─────────────────────────────────────────────────────────────────

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug: slugParts } = await params
  const slug = decodeSlugParts(slugParts)
  const slugs = parseSlugs(slug)
  if (!slugs) return { title: "Compare Models" }

  const [a, b] = await Promise.all([
    getModel(slugs[0]).catch(() => null),
    getModel(slugs[1]).catch(() => null),
  ])
  if (!a || !b) return { title: "Compare Models" }

  const { title, description, keywords } = buildCompareSeo(a, b)
  return {
    title,
    description,
    keywords,
    alternates: { canonical: `/compare/${slug}` },
    openGraph: { title, description },
  }
}

// ─── Page ──────────────────────────────────────────────────────────────────────

export default async function CompareStaticPage({ params }: PageProps) {
  const { slug: slugParts } = await params
  const slug = decodeSlugParts(slugParts)
  const slugs = parseSlugs(slug)
  if (!slugs) notFound()

  const [a, b] = await Promise.all([
    getModel(slugs[0]).catch(() => null),
    getModel(slugs[1]).catch(() => null),
  ])
  if (!a || !b) notFound()

  const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "ItemList",
    name: `${a.name} vs ${b.name} — Token Cost Comparison`,
    description: `Side-by-side pricing comparison: ${a.name} vs ${b.name}`,
    url: `${SITE_URL}/compare/${slug}`,
    numberOfItems: 2,
    itemListElement: [
      { "@type": "ListItem", position: 1, name: a.name, url: `${SITE_URL}/models/${a.slug}` },
      { "@type": "ListItem", position: 2, name: b.name, url: `${SITE_URL}/models/${b.slug}` },
    ],
  }

  const breadcrumbJsonLd = {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: [
      { "@type": "ListItem", position: 1, name: "Compare", item: `${SITE_URL}/compare` },
      {
        "@type": "ListItem",
        position: 2,
        name: `${a.name} vs ${b.name}`,
        item: `${SITE_URL}/compare/${slug}`,
      },
    ],
  }

  // Winner = lower price (input/output) or higher context window.
  // Null when both values are equal — no misleading ✓ badge is shown.
  const inputWinner:  "a" | "b" | null =
    a.input_price_per_m  < b.input_price_per_m  ? "a" : a.input_price_per_m  > b.input_price_per_m  ? "b" : null
  const outputWinner: "a" | "b" | null =
    a.output_price_per_m < b.output_price_per_m ? "a" : a.output_price_per_m > b.output_price_per_m ? "b" : null
  const ctxWinner:    "a" | "b" | null =
    a.context_window     > b.context_window     ? "a" : a.context_window     < b.context_window     ? "b" : null

  const rows: Array<{
    label: string
    aVal: string
    bVal: string
    winner: "a" | "b" | null
  }> = [
    {
      label:  "Input / 1M tokens",
      aVal:   formatPrice(a.input_price_per_m),
      bVal:   formatPrice(b.input_price_per_m),
      winner: inputWinner,
    },
    {
      label:  "Output / 1M tokens",
      aVal:   formatPrice(a.output_price_per_m),
      bVal:   formatPrice(b.output_price_per_m),
      winner: outputWinner,
    },
    {
      label:  "Context window",
      aVal:   formatContext(a.context_window),
      bVal:   formatContext(b.context_window),
      winner: ctxWinner,
    },
    {
      label:  "Provider",
      aVal:   formatProviderName(a.provider),
      bVal:   formatProviderName(b.provider),
      winner: null,
    },
    {
      label:  "Confidence",
      aVal:   a.trust.confidence.toUpperCase(),
      bVal:   b.trust.confidence.toUpperCase(),
      winner: null,
    },
  ]

  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: safeJsonLd(jsonLd) }} />
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: safeJsonLd(breadcrumbJsonLd) }} />

      <main
        className="mx-auto max-w-4xl px-4 sm:px-6 lg:px-8"
        style={{ paddingTop: "32px", paddingBottom: "64px" }}
      >
        {/* Breadcrumb */}
        <nav className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "16px" }}>
          <a href="/compare" style={{ color: "var(--muted)", textDecoration: "none" }}>Compare</a>
          <span style={{ margin: "0 6px" }}>›</span>
          <span style={{ color: "var(--text)" }}>{a.name} vs {b.name}</span>
        </nav>

        {/* Header */}
        <div style={{ marginBottom: "32px" }}>
          <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)" }}>
            {a.name} vs {b.name}
          </h1>
          <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
            Side-by-side token cost comparison · updated every 5 minutes
          </p>
        </div>

        {/* Column header row */}
        <div
          className="font-orbitron text-xs"
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 130px 130px",
            gap: "8px",
            padding: "8px 16px",
            color: "var(--dim)",
            borderBottom: "2px solid var(--borderDk)",
            textTransform: "uppercase",
            letterSpacing: "0.08em",
          }}
        >
          <span>Metric</span>
          <span style={{ textAlign: "right" }}>
            <a href={`/models/${a.slug}`} style={{ color: "inherit", textDecoration: "none" }}>
              {a.name}
            </a>
          </span>
          <span style={{ textAlign: "right" }}>
            <a href={`/models/${b.slug}`} style={{ color: "inherit", textDecoration: "none" }}>
              {b.name}
            </a>
          </span>
        </div>

        {/* Comparison rows */}
        {rows.map((row) => (
          <div
            key={row.label}
            className="font-outfit"
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 130px 130px",
              gap: "8px",
              padding: "12px 16px",
              borderBottom: "1px solid var(--border)",
              backgroundColor: "var(--surface)",
              alignItems: "center",
            }}
          >
            <div className="text-xs" style={{ color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>
              {row.label}
            </div>

            {/* Model A value */}
            <div
              className="font-orbitron text-xs"
              style={{
                textAlign: "right",
                padding: "6px 10px",
                border: `1px solid ${row.winner === "a" ? "var(--accent)" : "var(--border)"}`,
                backgroundColor: row.winner === "a" ? "var(--accentLt)" : "transparent",
                color: row.winner === "a" ? "var(--accentDk)" : "var(--text)",
              }}
            >
              {row.aVal}
              {row.winner === "a" && (
                <span style={{ marginLeft: "5px", fontSize: "10px" }}>✓</span>
              )}
            </div>

            {/* Model B value */}
            <div
              className="font-orbitron text-xs"
              style={{
                textAlign: "right",
                padding: "6px 10px",
                border: `1px solid ${row.winner === "b" ? "var(--accent)" : "var(--border)"}`,
                backgroundColor: row.winner === "b" ? "var(--accentLt)" : "transparent",
                color: row.winner === "b" ? "var(--accentDk)" : "var(--text)",
              }}
            >
              {row.bVal}
              {row.winner === "b" && (
                <span style={{ marginLeft: "5px", fontSize: "10px" }}>✓</span>
              )}
            </div>
          </div>
        ))}

        {/* CTAs */}
        <div style={{ marginTop: "32px", display: "flex", gap: "8px", flexWrap: "wrap" }}>
          <a
            href={`/compare?models=${encodeURIComponent(a.id)},${encodeURIComponent(b.id)}`}
            className="font-outfit text-sm"
            style={{
              padding: "10px 20px",
              border: "1px solid var(--accent)",
              backgroundColor: "var(--accent)",
              color: "white",
              textDecoration: "none",
            }}
          >
            Open in Compare Tool →
          </a>
          <a
            href="/compare"
            className="font-outfit text-sm"
            style={{
              padding: "10px 20px",
              border: "1px solid var(--border)",
              color: "var(--muted)",
              textDecoration: "none",
            }}
          >
            Compare other models
          </a>
        </div>
      </main>
    </>
  )
}
