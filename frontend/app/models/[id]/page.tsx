import type { Metadata } from "next"
import { getModel, getModelHistory } from "@/lib/api"

export const dynamic = "force-dynamic"
import PriceHistoryChart from "@/components/model/PriceHistoryChart"

interface PageProps {
  params: Promise<{ id: string }>
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { id } = await params
  const model = await getModel(id).catch(() => null)
  if (!model) return { title: "Model Not Found | LLMPrice" }
  return {
    title: `${model.name} Pricing | LLMPrice`,
    description: `Current and historical pricing for ${model.name} by ${model.provider}. Input: $${model.input_price_per_m.toFixed(4)}/1M tokens, Output: $${model.output_price_per_m.toFixed(4)}/1M tokens.`,
  }
}

function formatPrice(p: number): string { return `$${p.toFixed(4)}` }
function formatContext(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M`
  if (n >= 1_000)     return `${(n / 1_000).toFixed(0)}K`
  return String(n)
}

export default async function ModelDetailPage({ params }: PageProps) {
  const { id } = await params
  const [model, history] = await Promise.all([
    getModel(id),
    getModelHistory(id),
  ])

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

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
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
          <span
            className="font-outfit text-xs"
            style={{
              padding: "2px 8px",
              border: "1px solid var(--blueLt)",
              color: "var(--blue)",
              backgroundColor: "var(--blueLt)",
            }}
          >
            {model.provider}
          </span>
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
            { label: "Source",     value: model.trust.source                  },
            { label: "Confidence", value: model.trust.confidence.toUpperCase() },
          ].map(({ label, value }) => (
            <div key={label}>
              <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "4px" }}>{label}</div>
              <div className="font-orbitron text-sm" style={{ color: "var(--text)" }}>{value}</div>
            </div>
          ))}
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
