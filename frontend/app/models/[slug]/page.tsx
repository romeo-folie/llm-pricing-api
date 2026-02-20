import type { Metadata } from "next"
import { notFound, permanentRedirect } from "next/navigation"
import { getModel, getModelHistory } from "@/lib/api"
import { safeJsonLd } from "@/lib/utils"
import { formatPrice, formatContext, formatSourceName } from "@/lib/format"
import PriceHistoryChart from "@/components/model/PriceHistoryChart"

export const dynamic = "force-dynamic"

interface PageProps {
  params: Promise<{ slug: string }>
}

// ─── SEO keyword generation ───────────────────────────────────────────────────
// Builds title, description, and keyword list for a model page, covering the
// full range of search intents: per-token, per-1k, per-million, "how much does
// X cost", provider+model combos, and comparison prompts.

function buildModelSeo(model: {
  name: string
  provider: string
  slug: string
  input_price_per_m: number
  output_price_per_m: number
}) {
  const inputFmt  = formatPrice(model.input_price_per_m)
  const outputFmt = formatPrice(model.output_price_per_m)
  // Cost per 1 000 tokens — the unit developers most commonly search for
  const inputPer1k  = (model.input_price_per_m  / 1000).toFixed(6).replace(/\.?0+$/, "")
  const outputPer1k = (model.output_price_per_m / 1000).toFixed(6).replace(/\.?0+$/, "")

  const title = `${model.name} Pricing — ${inputFmt}/1M input tokens | LLMRates`

  const description =
    `${model.name} API pricing: ${inputFmt} per 1M input tokens, ` +
    `${outputFmt} per 1M output tokens ($${inputPer1k}/1k input, $${outputPer1k}/1k output). ` +
    `Compare ${model.provider} model costs, view full price history, and track real-time price changes.`

  const keywords = [
    `${model.name} pricing`,
    `${model.name} api price`,
    `${model.name} cost`,
    `${model.name} cost per token`,
    `${model.name} cost per 1000 tokens`,
    `${model.name} price per million tokens`,
    `${model.name} api cost`,
    `${model.name} token price`,
    `how much does ${model.name} cost`,
    `${model.name} api pricing`,
    `${model.provider} ${model.name} pricing`,
    `${model.provider} ${model.name} cost`,
    `${model.name} input output price`,
  ].join(", ")

  return { title, description, keywords }
}

// ─── Metadata ─────────────────────────────────────────────────────────────────

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug } = await params
  // Numeric IDs get a redirect in the page component; return minimal metadata here
  if (/^\d+$/.test(slug)) return { title: "Model", robots: { index: false, follow: true } }
  const model = await getModel(slug).catch(() => null)
  if (!model) return { title: "Model Not Found" }

  const { title, description, keywords } = buildModelSeo(model)

  return {
    title,
    description,
    keywords,
    alternates: { canonical: `/models/${model.slug}` },
    openGraph: {
      title: `${model.name} API Pricing — Token Cost & Price History`,
      description,
    },
  }
}

// ─── Page ──────────────────────────────────────────────────────────────────────

export default async function ModelDetailPage({ params }: PageProps) {
  const { slug } = await params

  // Legacy numeric-ID URLs (e.g. /models/42) → 308 permanent redirect to slug.
  // Only getModel() is wrapped; permanentRedirect() throws a Next.js internal
  // error that must propagate unmodified.
  if (/^\d+$/.test(slug)) {
    let model: Awaited<ReturnType<typeof getModel>> | null = null
    try {
      model = await getModel(slug)
    } catch {
      notFound()
    }
    permanentRedirect(`/models/${model!.slug}`)
  }

  const model = await getModel(slug).catch(() => null)
  if (!model) notFound()

  // Use numeric model.id for the history endpoint (integer-only backend route)
  const history = await getModelHistory(model.id).catch(() => [])

  const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "Product",
    name: model.name,
    description: `LLM API pricing for ${model.name} by ${model.provider}`,
    brand: { "@type": "Brand", name: model.provider },
    offers: {
      "@type": "Offer",
      priceCurrency: "USD",
      price: model.input_price_per_m,
      priceSpecification: [
        { "@type": "PriceSpecification", price: model.input_price_per_m,  name: "Input tokens per 1M"  },
        { "@type": "PriceSpecification", price: model.output_price_per_m, name: "Output tokens per 1M" },
      ],
    },
  }

  const breadcrumbJsonLd = {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: [
      {
        "@type": "ListItem",
        position: 1,
        name: "Models",
        item: `${SITE_URL}/models`,
      },
      {
        "@type": "ListItem",
        position: 2,
        name: model.name,
        item: `${SITE_URL}/models/${model.slug}`,
      },
    ],
  }

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(jsonLd) }}
      />
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(breadcrumbJsonLd) }}
      />

      <main
        className="mx-auto max-w-3xl px-4 sm:px-6 lg:px-8"
        style={{ paddingTop: "32px", paddingBottom: "64px" }}
      >
        {/* Breadcrumb */}
        <nav className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "16px" }}>
          <a href="/models" style={{ color: "var(--muted)", textDecoration: "none" }}>Models</a>
          <span style={{ margin: "0 6px" }}>›</span>
          <span style={{ color: "var(--text)" }}>{model.name}</span>
        </nav>

        {/* Title */}
        <h1 className="font-outfit text-2xl font-bold" style={{ color: "var(--ink)", marginBottom: "8px" }}>
          {model.name}
        </h1>
        <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginBottom: "24px" }}>
          <a
            href={`/providers/${encodeURIComponent(model.provider)}`}
            className="font-outfit text-xs"
            style={{
              padding: "2px 8px",
              border: "1px solid var(--blueLt)",
              color: "var(--blue)",
              backgroundColor: "var(--blueLt)",
              textDecoration: "none",
            }}
          >
            {model.provider}
          </a>
          <span
            className="font-outfit text-xs"
            style={{
              padding: "2px 8px",
              border: "1px solid var(--border)",
              color: "var(--muted)",
            }}
          >
            {model.modality}
          </span>
        </div>

        {/* Prices */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: "12px",
            marginBottom: "24px",
          }}
        >
          {[
            { label: "Input / 1M tokens",  value: formatPrice(model.input_price_per_m)  },
            { label: "Output / 1M tokens", value: formatPrice(model.output_price_per_m) },
          ].map(({ label, value }) => (
            <div
              key={label}
              style={{
                padding: "16px",
                border: "1px solid var(--border)",
                borderTop: "1px solid var(--border)",
                backgroundColor: "var(--surface)",
              }}
            >
              <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "8px" }}>{label}</div>
              <div className="font-orbitron text-2xl" style={{ color: "var(--ink)" }}>{value}</div>
            </div>
          ))}
        </div>

        {/* Meta */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(3, 1fr)",
            gap: "8px",
            marginBottom: "24px",
            padding: "12px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--surfaceLo)",
          }}
        >
          {[
            { label: "Context",    value: formatContext(model.context_window) },
            { label: "Source",     value: formatSourceName(model.trust.source) },
            { label: "Confidence", value: model.trust.confidence.toUpperCase() },
          ].map(({ label, value }) => (
            <div key={label}>
              <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "4px" }}>{label}</div>
              <div className="font-orbitron text-sm" style={{ color: "var(--text)" }}>{value}</div>
            </div>
          ))}
          {model.underlying_provider && (
            <div>
              <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "4px" }}>Via</div>
              <div className="font-orbitron text-sm" style={{ color: "var(--text)" }}>
                {model.underlying_provider.charAt(0).toUpperCase() + model.underlying_provider.slice(1)}
              </div>
            </div>
          )}
        </div>

        {/* Actions */}
        <div style={{ display: "flex", gap: "8px", marginBottom: "32px" }}>
          <a
            href={`/compare?models=${encodeURIComponent(model.id)}`}
            className="font-outfit text-sm"
            style={{
              padding: "8px 16px",
              border: "1px solid var(--accent)",
              backgroundColor: "var(--accent)",
              color: "white",
              textDecoration: "none",
            }}
          >
            Add to Compare
          </a>
          <a
            href={`/calculator?models=${encodeURIComponent(model.id)}`}
            className="font-outfit text-sm"
            style={{
              padding: "8px 16px",
              border: "1px solid var(--border)",
              color: "var(--muted)",
              textDecoration: "none",
            }}
          >
            Calculate Cost
          </a>
        </div>

        {/* Price history */}
        <h2
          className="font-orbitron text-xs"
          style={{
            color: "var(--dim)",
            textTransform: "uppercase",
            letterSpacing: "0.1em",
            marginBottom: "12px",
          }}
        >
          Price History
        </h2>
        <PriceHistoryChart history={history} />
      </main>
    </>
  )
}
