import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getChangesPage, getChangesSummary, getProviders } from "@/lib/api"
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

  const [changesResult, summaryResult, providersResult] = await Promise.allSettled([
    getChangesPage({ ...filter, limit: 50 }),
    getChangesSummary({ window: "7d", provider: filter.provider }),
    getProviders(),
  ])

  const changesPage = changesResult.status === "fulfilled" ? changesResult.value : null
  const summary     = summaryResult.status === "fulfilled" ? summaryResult.value : null
  const providers   = providersResult.status === "fulfilled" ? providersResult.value : []
  const apiUnavailable = changesResult.status === "rejected"

  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 md:px-8 lg:px-10"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
      {apiUnavailable && <ApiUnavailableBanner />}
      <Suspense fallback={null}>
        <ChangesFeed
          initialChanges={changesPage?.data ?? []}
          initialTotal={changesPage?.total ?? 0}
          initialHasMore={changesPage?.hasMore ?? false}
          initialNextCursor={changesPage?.nextCursor ?? null}
          initialSummary={summary}
          providers={providers}
          initialProvider={sp.provider ?? ""}
          initialSince={sp.since ?? ""}
        />
      </Suspense>
    </main>
  )
}
