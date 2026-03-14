import type { Metadata } from "next"
import { notFound } from "next/navigation"
import { getModels, getProviders } from "@/lib/api"
import { safeJsonLd } from "@/lib/utils"
import { formatPrice, formatContext, formatProviderName } from "@/lib/format"

export const dynamic = "force-dynamic"

interface PageProps {
  params: Promise<{ slug: string }>
}

// ─── SEO keyword generation ───────────────────────────────────────────────────

function buildProviderSeo(slug: string, displayName: string, modelCount: number) {
  const title = `${displayName} AI Model Pricing — All ${modelCount} Models & Token Costs | LLMRates`

  const description =
    `Compare all ${modelCount} ${displayName} AI model prices. ` +
    `Input and output token costs per 1M tokens for every ${displayName} model, ` +
    `with full price history, real-time updates, and confidence scores.`

  const keywords = [
    `${displayName} pricing`,
    `${displayName} api price`,
    `${displayName} models`,
    `${displayName} llm pricing`,
    `${displayName} token cost`,
    `${displayName} api cost`,
    `${displayName} model pricing`,
    `how much does ${displayName} cost`,
    `${displayName} model comparison`,
    `cheapest ${displayName} model`,
    `${displayName} price per token`,
    `${slug} pricing`,
    `${slug} api`,
  ].join(", ")

  return { title, description, keywords }
}

// ─── Metadata ─────────────────────────────────────────────────────────────────

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug } = await params
  const displayName = formatProviderName(slug)

  // Validate the provider exists by checking the providers list
  const providers = await getProviders().catch(() => [])
  const provider = providers.find((p) => p.id.toLowerCase() === slug.toLowerCase())
  if (!provider) return { title: "Provider Not Found" }

  const { title, description, keywords } = buildProviderSeo(slug, displayName, provider.model_count)

  return {
    title,
    description,
    keywords,
    alternates: { canonical: `/providers/${slug}` },
    openGraph: {
      title: `${displayName} AI Model Pricing — Token Costs & Price History`,
      description,
    },
  }
}

// ─── Page ──────────────────────────────────────────────────────────────────────

export default async function ProviderPage({ params }: PageProps) {
  const { slug } = await params
  const displayName = formatProviderName(slug)

  const [models, providers] = await Promise.all([
    getModels({ provider: slug }).catch(() => []),
    getProviders().catch(() => []),
  ])

  const provider = providers.find((p) => p.id.toLowerCase() === slug.toLowerCase())
  if (!provider && models.length === 0) notFound()

  const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "ItemList",
    name: `${displayName} AI Models`,
    description: `Pricing for all ${displayName} AI models`,
    url: `${SITE_URL}/providers/${slug}`,
    numberOfItems: models.length,
    itemListElement: models.slice(0, 50).map((m, i) => ({
      "@type": "ListItem",
      position: i + 1,
      name: m.name,
      url: `${SITE_URL}/models/${m.slug}`,
    })),
  }

  const breadcrumbJsonLd = {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: [
      { "@type": "ListItem", position: 1, name: "Models", item: `${SITE_URL}/models` },
      { "@type": "ListItem", position: 2, name: displayName, item: `${SITE_URL}/providers/${slug}` },
    ],
  }

  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: safeJsonLd(jsonLd) }} />
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: safeJsonLd(breadcrumbJsonLd) }} />

      <main
        className="mx-auto max-w-5xl px-4 sm:px-6 lg:px-8"
        style={{ paddingTop: "32px", paddingBottom: "64px" }}
      >
        {/* Breadcrumb */}
        <nav className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "16px" }}>
          <a href="/models" style={{ color: "var(--muted)", textDecoration: "none" }}>Models</a>
          <span style={{ margin: "0 6px" }}>›</span>
          <span style={{ color: "var(--text)" }}>{displayName}</span>
        </nav>

        {/* Header */}
        <div style={{ marginBottom: "32px" }}>
          <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)" }}>
            {displayName} Model Pricing
          </h1>
          <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
            {models.length} model{models.length !== 1 ? "s" : ""} · input &amp; output costs per 1M tokens · updated every 5 minutes
          </p>
        </div>

        {/* Model table */}
        {models.length === 0 ? (
          <p className="font-outfit text-sm" style={{ color: "var(--muted)" }}>
            No pricing data available for this provider.
          </p>
        ) : (
          <>
            {/* Column headers */}
            <div
              className="font-orbitron text-xs"
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 80px 130px 130px 70px",
                gap: "8px",
                padding: "8px 16px",
                color: "var(--dim)",
                borderBottom: "2px solid var(--borderDk)",
                textTransform: "uppercase",
                letterSpacing: "0.08em",
              }}
            >
              <span>Model</span>
              <span style={{ textAlign: "right" }}>Context</span>
              <span style={{ textAlign: "right" }}>Input / 1M</span>
              <span style={{ textAlign: "right" }}>Output / 1M</span>
              <span style={{ textAlign: "right" }}>Data</span>
            </div>

            {models.map((m) => (
              <a
                key={m.id}
                href={`/models/${m.slug}`}
                style={{ textDecoration: "none" }}
              >
                <div
                  className="font-outfit"
                  style={{
                    display: "grid",
                    gridTemplateColumns: "1fr 80px 130px 130px 70px",
                    gap: "8px",
                    padding: "12px 16px",
                    borderBottom: "1px solid var(--border)",
                    backgroundColor: "var(--surface)",
                    transition: "background-color 0.1s",
                    alignItems: "center",
                  }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.backgroundColor = "var(--surfaceLo)" }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.backgroundColor = "var(--surface)" }}
                >
                  <div>
                    <div className="text-sm" style={{ color: "var(--ink)", fontWeight: 600 }}>{m.name}</div>
                    <div className="text-xs" style={{ color: "var(--muted)" }}>{m.modality}</div>
                  </div>
                  <div className="font-orbitron text-xs" style={{ color: "var(--dim)", textAlign: "right" }}>
                    {formatContext(m.context_window)}
                  </div>
                  <div className="font-orbitron text-xs" style={{ color: "var(--text)", textAlign: "right" }}>
                    {formatPrice(m.input_price_per_m)}
                  </div>
                  <div className="font-orbitron text-xs" style={{ color: "var(--muted)", textAlign: "right" }}>
                    {formatPrice(m.output_price_per_m)}
                  </div>
                  <div style={{ textAlign: "right" }}>
                    <span
                      className="font-orbitron text-xs"
                      style={{
                        padding: "2px 5px",
                        border: `1px solid ${m.trust.confidence === "high" ? "var(--green)" : m.trust.confidence === "medium" ? "var(--accent)" : "var(--red)"}`,
                        color: m.trust.confidence === "high" ? "var(--green)" : m.trust.confidence === "medium" ? "var(--accent)" : "var(--red)",
                      }}
                    >
                      {m.trust.confidence.toUpperCase()}
                    </span>
                  </div>
                </div>
              </a>
            ))}
          </>
        )}

        {/* Compare CTA */}
        {models.length >= 2 && (
          <div style={{ marginTop: "32px" }}>
            <a
              href={`/compare?models=${models.slice(0, 3).map((m) => encodeURIComponent(m.id)).join(",")}`}
              className="font-outfit text-sm"
              style={{
                display: "inline-block",
                padding: "8px 16px",
                border: "1px solid var(--accent)",
                backgroundColor: "var(--accent)",
                color: "white",
                textDecoration: "none",
              }}
            >
              Compare top {Math.min(3, models.length)} {displayName} models →
            </a>
          </div>
        )}
      </main>
    </>
  )
}
