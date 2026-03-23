import { Suspense } from "react"
import type { Metadata } from "next"
import CompareClient from "@/components/compare/CompareClient"
import { USE_CASES } from "@/lib/use-cases"

export const dynamic = "force-dynamic"

interface PageProps {
  searchParams: Promise<{ "use-case"?: string; models?: string }>
}

export async function generateMetadata({ searchParams }: PageProps): Promise<Metadata> {
  const sp = await searchParams
  const ucSlug = sp["use-case"]
  const models = sp.models

  const uc = ucSlug ? USE_CASES.find((u) => u.slug === ucSlug) : null
  const modelSlugs = models ? models.split(",").filter(Boolean) : []

  let title = "Compare AI Models | LLMRates"
  let description =
    "Compare GPT-4, Claude, Gemini, and Mistral pricing side by side. See input/output token costs, context windows, and capability scores across AI providers."

  if (uc && modelSlugs.length >= 2) {
    const [a, b] = modelSlugs
    title = `${a} vs ${b} for ${uc.label} | LLMRates`
    description = `Side-by-side comparison of ${a} and ${b} for ${uc.label.toLowerCase()} tasks. Pricing, capability scores, and benchmarks.`
  } else if (uc) {
    title = `Best Models for ${uc.label} | LLMRates`
    description = `Discover and compare the best AI models for ${uc.label.toLowerCase()}. Ranked by capability scores and pricing.`
  }

  const params = new URLSearchParams()
  if (ucSlug) params.set("use-case", ucSlug)
  if (models) params.set("models", models)
  const qs = params.toString()
  const canonical = `/compare${qs ? `?${qs}` : ""}`

  return {
    title,
    description,
    alternates: { canonical },
    openGraph: { title, description },
  }
}

export default async function ComparePage() {
  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      <Suspense fallback={null}>
        <CompareClient />
      </Suspense>
    </main>
  )
}
