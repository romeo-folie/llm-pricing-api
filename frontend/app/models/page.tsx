import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModels, getProviders } from "@/lib/api"
import ModelCard from "@/components/model/ModelCard"
import ModelDetailModal from "@/components/model/ModelDetailModal"

export const metadata: Metadata = {
  title: "LLM Model Browser | LLMPrice",
  description:
    "Browse current pricing for 200+ LLM models across OpenAI, Anthropic, Google, Mistral and more. Filter by provider, modality, and context window.",
}

interface PageProps {
  searchParams: Promise<{ provider?: string; modality?: string; min_context?: string; model?: string }>
}

const MODALITIES = ["text", "vision", "audio", "code", "embedding"]

export default async function ModelsPage({ searchParams }: PageProps) {
  const sp = await searchParams
  const filter = {
    provider:    sp.provider    || undefined,
    modality:    sp.modality    || undefined,
    min_context: sp.min_context ? Number(sp.min_context) : undefined,
  }

  const [models, providers] = await Promise.all([getModels(filter), getProviders()])

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "Dataset",
    name: "LLM Token Pricing Database",
    description: "Reconciled pricing data for large language models across major providers.",
    url: "https://llmprice.dev/models",
    creator: { "@type": "Organization", name: "LLMPrice" },
    license: "https://creativecommons.org/licenses/by/4.0/",
  }

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />

      <main
        className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
        style={{ paddingTop: "32px", paddingBottom: "64px" }}
      >
        {/* Header */}
        <div style={{ marginBottom: "24px" }}>
          <h1
            className="font-orbitron text-2xl font-bold"
            style={{ color: "var(--ink)" }}
          >
            Model Browser
          </h1>
          <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
            {models.length} models tracked · updated every 5 minutes
          </p>
        </div>

        {/* Filters */}
        <form
          method="GET"
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: "8px",
            marginBottom: "20px",
            padding: "12px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--surfaceLo)",
            borderRadius: "2px",
          }}
        >
          {/* Provider */}
          <select
            name="provider"
            defaultValue={sp.provider ?? ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 10px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: "var(--surface)",
              color: "var(--text)",
            }}
          >
            <option value="">All providers</option>
            {providers.map((p) => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </select>

          {/* Modality */}
          <select
            name="modality"
            defaultValue={sp.modality ?? ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 10px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: "var(--surface)",
              color: "var(--text)",
            }}
          >
            <option value="">All modalities</option>
            {MODALITIES.map((m) => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>

          {/* Min context */}
          <select
            name="min_context"
            defaultValue={sp.min_context ?? ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 10px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              backgroundColor: "var(--surface)",
              color: "var(--text)",
            }}
          >
            <option value="">Any context</option>
            <option value="4096">4K+</option>
            <option value="32768">32K+</option>
            <option value="128000">128K+</option>
            <option value="1000000">1M+</option>
          </select>

          <button
            type="submit"
            className="font-outfit text-sm"
            style={{
              padding: "6px 16px",
              border: "1px solid var(--accent)",
              borderRadius: "2px",
              backgroundColor: "var(--accent)",
              color: "white",
              cursor: "pointer",
            }}
          >
            Filter
          </button>

          {(sp.provider || sp.modality || sp.min_context) && (
            <a
              href="/models"
              className="font-outfit text-sm"
              style={{
                padding: "6px 12px",
                border: "1px solid var(--border)",
                borderRadius: "2px",
                color: "var(--muted)",
                textDecoration: "none",
                display: "inline-flex",
                alignItems: "center",
              }}
            >
              Clear
            </a>
          )}
        </form>

        {/* Column headers */}
        <div
          className="font-orbitron text-xs"
          style={{
            display: "flex",
            padding: "8px 16px",
            color: "var(--dim)",
            borderBottom: "2px solid var(--borderDk)",
            gap: "12px",
            alignItems: "center",
            textTransform: "uppercase",
            letterSpacing: "0.08em",
          }}
        >
          <span style={{ width: "8px" }} />
          <span style={{ flex: 1 }}>Model</span>
          <span>Status</span>
          <span style={{ minWidth: "36px", textAlign: "right" }}>Ctx</span>
          <span style={{ minWidth: "100px", textAlign: "right" }}>Price /1M</span>
        </div>

        {/* Model list */}
        {models.length === 0 ? (
          <div
            className="font-outfit text-sm"
            style={{
              textAlign: "center",
              padding: "64px 24px",
              color: "var(--muted)",
              borderBottom: "1px solid var(--border)",
            }}
          >
            No models match the current filters.
          </div>
        ) : (
          <div>
            {models.map((model) => (
              <Suspense key={model.id} fallback={null}>
                <ModelCard model={model} />
              </Suspense>
            ))}
          </div>
        )}
      </main>

      {/* Modal — reads ?model= param */}
      <Suspense fallback={null}>
        <ModelDetailModal />
      </Suspense>
    </>
  )
}
