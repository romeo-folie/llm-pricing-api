import { Suspense } from "react"
import type { Metadata } from "next"
import { getModels } from "@/lib/api"
import CompareClient from "@/components/compare/CompareClient"

export async function generateMetadata(): Promise<Metadata> {
  const title = "LLM Pricing Comparison | Compare AI Model Costs"
  const description =
    "Compare LLM pricing and AI model costs side by side. Evaluate input and output token prices, context windows, trust data, and capability scores across providers."

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
