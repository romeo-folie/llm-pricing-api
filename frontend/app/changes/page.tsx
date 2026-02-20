import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getChanges, getProviders } from "@/lib/api"
import ChangesFeed from "@/components/changes/ChangesFeed"
import ApiUnavailableBanner from "@/components/ui/ApiUnavailableBanner"

export const metadata: Metadata = {
  title: "AI Model Price Changes — Real-Time LLM Pricing Updates",
  description:
    "Track AI model price changes in real time. See when GPT, Claude, Gemini, and Mistral pricing changes. Updated every 5 minutes from OpenRouter, LiteLLM, and Hugging Face.",
  alternates: { canonical: "/changes" },
  openGraph: {
    title: "AI Model Price Changes — Live LLM Pricing Feed",
    description:
      "Real-time feed of AI model pricing changes. Track LLM cost updates across all major providers.",
  },
}

interface PageProps {
  searchParams: Promise<{ provider?: string; since?: string }>
}

export default async function ChangesPage({ searchParams }: PageProps) {
  const sp = await searchParams
  const filter = {
    provider: sp.provider || undefined,
    since:    sp.since    || undefined,
  }

  const [changesResult, providersResult] = await Promise.allSettled([
    getChanges(filter),
    getProviders(),
  ])
  const changes   = changesResult.status   === "fulfilled" ? changesResult.value   : []
  const providers = providersResult.status === "fulfilled" ? providersResult.value : []
  const apiUnavailable = changesResult.status === "rejected"

  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      {apiUnavailable && <ApiUnavailableBanner />}
      <Suspense fallback={null}>
        <ChangesFeed
          initialChanges={changes}
          providers={providers}
          initialProvider={sp.provider ?? ""}
          initialSince={sp.since ?? ""}
        />
      </Suspense>
    </main>
  )
}
