import { Suspense } from "react"
import type { Metadata } from "next"
import { getModels } from "@/lib/api"
import CompareClient from "@/components/compare/CompareClient"

export async function generateMetadata(): Promise<Metadata> {
  const title = "Compare AI Models | LLMRates"
  const description =
    "Compare GPT-4, Claude, Gemini, and Mistral pricing side by side. See input/output token costs, context windows, and capability scores across AI providers."

  return {
    title,
    description,
    alternates: { canonical: "/compare" },
    openGraph: { title, description },
  }
}

export default async function ComparePage() {
  const allModels = await getModels().catch(() => [])
  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      <Suspense fallback={null}>
        <CompareClient allModels={allModels} />
      </Suspense>
    </main>
  )
}
