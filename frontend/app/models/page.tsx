import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModelsPaginated, getProviders } from "@/lib/api"
import { safeJsonLd } from "@/lib/utils"
import ModelCard from "@/components/model/ModelCard"
import ModelDetailModal from "@/components/model/ModelDetailModal"
import Pagination from "@/components/ui/Pagination"
import ApiUnavailableBanner from "@/components/ui/ApiUnavailableBanner"

export const metadata: Metadata = {
  title: "AI Model Pricing — GPT-4o, Claude, Gemini, Grok Token Costs | LLMRates",
  description:
    "Browse token pricing for 300+ AI models: GPT-4o, Claude 3.5, Gemini 1.5, Grok, Mistral, and more. " +
    "Compare input/output costs per 1M tokens, filter by provider and context window. " +
    "Reconciled from multiple sources and updated every 5 minutes.",
  keywords:
    "ai model pricing, llm pricing, gpt-4o price, claude pricing, gemini pricing, grok pricing, " +
    "mistral pricing, openai token cost, anthropic api price, llm api cost comparison, " +
    "cost per 1000 tokens, cheapest llm, ai api pricing 2025",
  alternates: { canonical: "/models" },
  openGraph: {
    title: "AI Model Pricing Browser — Compare LLM Token Costs",
    description:
      "Browse reconciled pricing for 300+ AI models. Compare GPT-4o, Claude 3.5, Gemini, Grok token costs.",
  },
}

interface PageProps {
  searchParams: Promise<{
    provider?: string
    modality?: string
    min_context?: string
    model?: string
    page?: string
  }>
}

const MODALITIES = ["text", "multimodal", "image", "audio", "embedding"]
const PER_PAGE = 24

export default async function ModelsPage({ searchParams }: PageProps) {
  const sp = await searchParams
  const filter = {
    provider:    sp.provider    || undefined,
    modality:    sp.modality    || undefined,
    min_context: sp.min_context ? Number(sp.min_context) : undefined,
  }

  const requestedPage = sp.page ? Math.max(1, Number(sp.page) || 1) : 1

  const [modelsResult, providersResult] = await Promise.allSettled([
    getModelsPaginated({ ...filter, page: requestedPage, per_page: PER_PAGE }),
    getProviders(),
  ])
  const { data: models, total } = modelsResult.status === "fulfilled"
    ? modelsResult.value
    : { data: [], total: 0 }
  const providers = providersResult.status === "fulfilled" ? providersResult.value : []
  const apiUnavailable = modelsResult.status === "rejected"

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE))
  // Clamp page to valid range
  const page = Math.min(requestedPage, totalPages)
  const rangeStart = (page - 1) * PER_PAGE + 1
  const rangeEnd = Math.min(page * PER_PAGE, total)

  // Preserve current filters in pagination links
  const paginationParams: Record<string, string> = {}
  if (sp.provider) paginationParams.provider = sp.provider
  if (sp.modality) paginationParams.modality = sp.modality
  if (sp.min_context) paginationParams.min_context = sp.min_context

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "Dataset",
    name: "LLM Token Pricing Database",
    description: "Reconciled pricing data for large language models across major providers.",
    url: "https://llmrates.live/models",
    creator: { "@type": "Organization", name: "LLMRates" },
    license: "https://creativecommons.org/licenses/by/4.0/",
  }

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(jsonLd) }}
      />

      <main
        className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
        style={{ paddingTop: "32px", paddingBottom: "64px" }}
      >
        {/* API unavailable banner */}
        {apiUnavailable && <ApiUnavailableBanner />}

        {/* Header */}
        <div style={{ marginBottom: "24px" }}>
          <span
            className="font-orbitron text-xs tracking-widest"
            style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
          >
            [ MODEL BROWSER ]
          </span>
          <h1
            className="font-outfit text-2xl font-bold"
            style={{ color: "var(--ink)" }}
          >
            Model Browser
          </h1>
          <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
            {apiUnavailable
              ? "API unavailable"
              : `${total} models tracked · updated every 5 minutes`}
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

        {/* Result count */}
        {!apiUnavailable && total > 0 && (
          <p
            className="font-outfit text-sm"
            style={{ color: "var(--muted)", marginBottom: "12px" }}
          >
            Showing {rangeStart}–{rangeEnd} of {total} models
          </p>
        )}

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
            {apiUnavailable ? "No data available — API is unreachable." : "No models match the current filters."}
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

        {/* Pagination */}
        <Pagination
          currentPage={page}
          totalPages={totalPages}
          searchParams={paginationParams}
        />
      </main>

      {/* Modal — reads ?model= param */}
      <Suspense fallback={null}>
        <ModelDetailModal />
      </Suspense>
    </>
  )
}
