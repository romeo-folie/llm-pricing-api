import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModels, getProviders, type Model } from "@/lib/api"
import {
  MIN_CONTEXT_OPTIONS,
  MODALITIES,
  paginate,
  parseModelsSearchParams,
  resolveProvider,
  type ModelsSearchParams,
} from "@/lib/models-filter"
import { safeJsonLd } from "@/lib/utils"
import ModelCard from "@/components/model/ModelCard"
import ModelDetailModal from "@/components/model/ModelDetailModal"
import Pagination from "@/components/ui/Pagination"
import ApiUnavailableBanner from "@/components/ui/ApiUnavailableBanner"

export const metadata: Metadata = {
  title: "AI Model Pricing | Compare LLM Token Costs",
  description:
    "Browse current AI model pricing across providers. Compare LLM input and output token prices, API costs per million tokens, context windows, and source data.",
  keywords:
    "ai model pricing, llm pricing, llm prices, llm costs, token pricing, token prices, " +
    "input token cost, output token cost, ai api pricing, ai api costs, price per million tokens, " +
    "llm pricing comparison, cheapest llm api",
  alternates: { canonical: "/models" },
  openGraph: {
    title: "AI Model Pricing Browser — Compare LLM Token Costs",
    description:
      "Browse current AI model pricing and compare input and output token costs across providers.",
  },
}

interface PageProps {
  searchParams: Promise<ModelsSearchParams & { model?: string | string[] }>
}

const PER_PAGE = 24

export default async function ModelsPage({ searchParams }: PageProps) {
  const f = parseModelsSearchParams(await searchParams)

  // getModels() serves every search from one cached catalog per filter set;
  // q and page are applied in memory so they never become cache keys. The
  // provider must be a real one, or arbitrary values would each cost a write.
  // Only a provider filter has to wait for the provider list.
  const providersPromise = getProviders().catch(() => [])
  const provider = f.provider ? resolveProvider(f.provider, await providersPromise) : undefined
  const [modelsResult, providers] = await Promise.all([
    getModels({ provider, modality: f.modality, min_context: f.min_context, q: f.q }).then(
      (models) => ({ models, failed: false }),
      () => ({ models: [] as Model[], failed: true }),
    ),
    providersPromise,
  ])
  const matching = modelsResult.models
  const apiUnavailable = modelsResult.failed

  const total = matching.length
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE))
  const page = Math.min(f.page, totalPages)
  const { data: models } = paginate(matching, page, PER_PAGE)
  const rangeStart = (page - 1) * PER_PAGE + 1
  const rangeEnd = Math.min(page * PER_PAGE, total)

  // Preserve current filters in pagination links
  const paginationParams: Record<string, string> = {}
  if (provider) paginationParams.provider = provider
  if (f.modality) paginationParams.modality = f.modality
  if (f.min_context) paginationParams.min_context = String(f.min_context)
  if (f.q) paginationParams.q = f.q

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
        <div className="animate-wireframe-fade" style={{ marginBottom: "24px", animationDelay: "1.2s" }}>
          <h1
            className="font-outfit text-2xl font-bold"
            style={{ color: "var(--ink)" }}
          >
            AI Model Pricing
          </h1>
          <p className="font-outfit text-sm" style={{ color: "var(--muted)", marginTop: "4px" }}>
            {apiUnavailable
              ? "API unavailable"
              : `Compare token prices and API costs across ${total} models · source timestamps included`}
          </p>
        </div>

        {/* Filters */}
        <form
          method="GET"
          className="animate-draw-border-box animate-wireframe-fade"
          style={{
            marginBottom: "20px",
            padding: "16px 20px",
            backgroundColor: "var(--surfaceLo)",
            "--draw-delay": "1.3s",
            animationDelay: "1.3s",
          } as React.CSSProperties}
        >
          <div className="flex flex-wrap" style={{ gap: "8px", alignItems: "center" }}>
          
          {/* Text search */}
          <input
            type="text"
            name="q"
            defaultValue={f.q ?? ""}
            placeholder="Search models..."
            className="font-outfit text-sm"
            style={{
              padding: "6px 12px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--bg)",
              color: "var(--ink)",
              outline: "none",
              flex: "1 1 max-content",
            }}
          />

          {/* Provider */}
          <select
            name="provider"
            defaultValue={provider ?? ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 32px 6px 12px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--bg)",
              color: "var(--ink)",
              cursor: "pointer",
              outline: "none",
              appearance: "none",
              backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2378716C' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
              backgroundRepeat: "no-repeat",
              backgroundPosition: "right 10px center",
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
            defaultValue={f.modality ?? ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 32px 6px 12px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--bg)",
              color: "var(--ink)",
              cursor: "pointer",
              outline: "none",
              appearance: "none",
              backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2378716C' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
              backgroundRepeat: "no-repeat",
              backgroundPosition: "right 10px center",
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
            defaultValue={f.min_context ? String(f.min_context) : ""}
            className="font-outfit text-sm"
            style={{
              padding: "6px 32px 6px 12px",
              border: "1px solid var(--border)",
              backgroundColor: "var(--bg)",
              color: "var(--ink)",
              cursor: "pointer",
              outline: "none",
              appearance: "none",
              backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2378716C' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
              backgroundRepeat: "no-repeat",
              backgroundPosition: "right 10px center",
            }}
          >
            <option value="">Any context</option>
            {MIN_CONTEXT_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
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

          {(provider || f.modality || f.min_context || f.q) && (
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
          </div>
        </form>

        {/* Result count */}
        {!apiUnavailable && total > 0 && (
          <p
            className="font-outfit text-sm animate-wireframe-fade"
            style={{ color: "var(--muted)", marginBottom: "12px", animationDelay: "1.35s" }}
          >
            Showing {rangeStart}–{rangeEnd} of {total} models
          </p>
        )}

        {/* Column headers */}
        <div
          className="font-orbitron text-xs animate-draw-border-b-dk"
          style={{
            padding: "8px 16px",
            "--draw-delay": "0.7s"
          } as React.CSSProperties}
        >
          <div className="animate-wireframe-fade" style={{ display: "flex", alignItems: "center", gap: "12px", width: "100%", color: "var(--dim)", textTransform: "uppercase", letterSpacing: "0.08em", animationDelay: "1.4s" }}>
            <span style={{ flex: 1 }}>Model</span>
            <span>Status</span>
            <span style={{ minWidth: "36px", textAlign: "right" }}>Ctx</span>
            <span style={{ minWidth: "100px", textAlign: "right" }}>Price /1M</span>
          </div>
        </div>

        {/* Model list */}
        {models.length === 0 ? (
          <div
            className="font-outfit text-sm animate-draw-border-b"
            style={{
              textAlign: "center",
              "--draw-delay": "0.8s"
            } as React.CSSProperties}
          >
            <div className="animate-wireframe-fade" style={{ padding: "64px 24px", color: "var(--muted)", animationDelay: "1.5s" }}>
              {apiUnavailable ? "No data available — API is unreachable." : "No models match the current filters."}
            </div>
          </div>
        ) : (
          <div>
            {models.map((model, index) => (
              <Suspense key={model.id} fallback={null}>
                <ModelCard model={model} index={index} />
              </Suspense>
            ))}
          </div>
        )}

        {/* Pagination */}
        <div className="animate-wireframe-fade" style={{ animationDelay: "1.6s" }}>
          <Pagination
            currentPage={page}
            totalPages={totalPages}
            searchParams={paginationParams}
          />
        </div>
      </main>

      {/* Modal — reads ?model= param */}
      <Suspense fallback={null}>
        <ModelDetailModal />
      </Suspense>
    </>
  )
}
