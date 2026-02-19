import { Suspense } from "react"
import type { Metadata } from "next"

export const dynamic = "force-dynamic"
import { getChanges, getProviders } from "@/lib/api"
import ChangesFeed from "@/components/changes/ChangesFeed"

export const metadata: Metadata = {
  title: "Price Changes",
  description:
    "Real-time LLM token pricing changes tracked across providers. Updated every 5 minutes from OpenRouter, LiteLLM, and provider docs.",
  openGraph: {
    title: "LLM Price Changes",
    description:
      "Live feed of LLM pricing changes across OpenRouter, LiteLLM, and provider docs.",
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
      {apiUnavailable && (
        <div
          className="font-outfit text-sm"
          style={{
            padding: "10px 14px",
            marginBottom: "20px",
            border: "1px solid var(--border)",
            borderLeft: "3px solid var(--yellow)",
            backgroundColor: "var(--yellowLt)",
            color: "var(--muted)",
          }}
        >
          ⚠ Pricing API is currently unavailable — data will appear once connectivity is restored.
        </div>
      )}
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
