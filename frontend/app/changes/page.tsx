import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getChanges, getProviders } from "@/lib/api"
import ChangesFeed from "@/components/changes/ChangesFeed"

export const metadata: Metadata = {
  title: "LLM Price Changes | LLMPrice",
  description: "Real-time LLM token pricing changes tracked across providers. Updated every 5 minutes from OpenRouter, LiteLLM, and provider docs.",
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

  const [changes, providers] = await Promise.all([
    getChanges(filter),
    getProviders(),
  ])

  return (
    <main
      className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"
      style={{ paddingTop: "32px", paddingBottom: "64px" }}
    >
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
