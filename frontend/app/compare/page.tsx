import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getModels, getCompare } from "@/lib/api"
import CompareClient from "@/components/compare/CompareClient"

export const metadata: Metadata = {
  title: "Compare LLM Models | LLMPrice",
  description: "Side-by-side LLM pricing comparison. Compare input/output token prices, context windows, and confidence across providers.",
}

interface PageProps {
  searchParams: Promise<{ models?: string }>
}

export default async function ComparePage({ searchParams }: PageProps) {
  const sp  = await searchParams
  const ids = sp.models ? sp.models.split(",").filter(Boolean) : []

  const [allModels, compareData] = await Promise.all([
    getModels(),
    ids.length > 0 ? getCompare(ids) : Promise.resolve([]),
  ])

  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      <Suspense fallback={null}>
        <CompareClient
          allModels={allModels}
          initialCompare={compareData}
          initialIds={ids}
        />
      </Suspense>
    </main>
  )
}
