import type { Metadata } from "next"
import Link from "next/link"
import { notFound } from "next/navigation"
import { USE_CASES, USE_CASE_SLUGS } from "@/lib/use-cases"
import { safeJsonLd } from "@/lib/utils"
import BestForClient from "@/components/best-for/BestForClient"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

interface PageProps {
  params: Promise<{ "use-case": string }>
}

export function generateStaticParams() {
  return USE_CASE_SLUGS.map((slug) => ({ "use-case": slug }))
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { "use-case": slug } = await params
  const uc = USE_CASES.find((u) => u.slug === slug)
  if (!uc) return { title: "Not Found" }
  return {
    title: `Best Models for ${uc.label} | LLMRates`,
    description: `Compare the top AI models for ${uc.label.toLowerCase()} tasks. Benchmark-backed rankings with score breakdowns and rationale.`,
    openGraph: {
      title: `Best Models for ${uc.label}`,
      description: `Compare the top AI models for ${uc.label.toLowerCase()} tasks. Benchmark-backed rankings with score breakdowns and rationale.`,
      url: `${SITE_URL}/best-for/${uc.slug}`,
    },
    alternates: {
      canonical: `${SITE_URL}/best-for/${uc.slug}`,
    },
  }
}

export default async function BestForUseCasePage({ params }: PageProps) {
  const { "use-case": slug } = await params
  const uc = USE_CASES.find((u) => u.slug === slug)
  if (!uc) notFound()

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "WebPage",
    name: `Best Models for ${uc.label}`,
    description: `Compare the top AI models for ${uc.label.toLowerCase()} tasks. Benchmark-backed rankings.`,
    url: `${SITE_URL}/best-for/${uc.slug}`,
  }

  return (
    <main className="mx-auto max-w-[1280px] px-4 py-8">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(jsonLd) }}
      />

      {/* Breadcrumb */}
      <nav className="flex items-center gap-2 text-sm mb-6" aria-label="Breadcrumb">
        <Link href="/best-for" style={{ color: "var(--muted)" }} className="hover:underline">
          Best For
        </Link>
        <span style={{ color: "var(--muted)" }}>/</span>
        <span style={{ color: "var(--ink)" }}>{uc.label}</span>
      </nav>

      <div className="flex items-center gap-3 mb-2">
        <span className="text-3xl" role="img" aria-label={uc.label}>
          {uc.icon}
        </span>
        <h1
          className="font-orbitron text-2xl font-bold tracking-tight"
          style={{ color: "var(--ink)" }}
        >
          Best Models for {uc.label}
        </h1>
      </div>
      <p className="text-sm mb-8" style={{ color: "var(--muted)" }}>
        {uc.description}
      </p>

      <BestForClient useCase={uc} />
    </main>
  )
}
