import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModels } from "@/lib/api"
import CalculatorClient from "@/components/calculator/CalculatorClient"

export const metadata: Metadata = {
  title: "LLM Cost Calculator | LLMPrice",
  description: "Calculate your LLM API costs by token volume. Compare daily, monthly, and yearly spend across OpenAI, Anthropic, Google, Mistral and more.",
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
