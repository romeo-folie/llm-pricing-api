import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModels } from "@/lib/api"
import CalculatorClient from "@/components/calculator/CalculatorClient"

export const metadata: Metadata = {
  title: "AI API Cost Calculator — Estimate LLM Token Spend",
  description:
    "Calculate AI API costs by token volume. Estimate daily, monthly, and yearly spend for GPT-4, Claude, Gemini, and Mistral. Compare pricing across providers.",
  alternates: { canonical: "/calculator" },
  openGraph: {
    title: "AI API Cost Calculator — LLM Token Pricing Estimator",
    description:
      "Estimate your AI API spend by token volume. Daily, monthly, and yearly cost projections across major LLM providers.",
  },
}

export default async function CalculatorPage() {
  const allModels = await getModels().catch(() => [])

  return (
    <main
      className="mx-auto max-w-4xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      <Suspense fallback={null}>
        <CalculatorClient allModels={allModels} />
      </Suspense>
    </main>
  )
}
