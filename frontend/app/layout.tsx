import type { Metadata } from "next"
import { GeistSans } from "geist/font/sans"
import { GeistMono } from "geist/font/mono"
import Nav from "@/components/layout/Nav"
import Footer from "@/components/layout/Footer"
import GoogleAnalytics from "@/components/analytics/GoogleAnalytics"
import { safeJsonLd } from "@/lib/utils"
import "./globals.css"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

const websiteJsonLd = {
  "@context": "https://schema.org",
  "@type": "WebSite",
  name: "LLMRates",
  url: SITE_URL,
  description:
    "Reconciled, multi-source LLM token pricing with full price history. Compare AI model costs, track price changes, and access agent-optimized APIs.",
  potentialAction: {
    "@type": "SearchAction",
    target: `${SITE_URL}/models?q={search_term_string}`,
    "query-input": "required name=search_term_string",
  },
}

const organizationJsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  name: "LLMRates",
  url: SITE_URL,
  description:
    "Reconciled LLM token pricing platform. Multi-source verified AI model pricing with full history and agent-optimized APIs.",
  sameAs: [],
}

export const metadata: Metadata = {
  title: {
    default: "LLMRates — AI Model Pricing API & Cost Comparison",
    template: "%s — LLMRates",
  },
  description:
    "Compare AI model pricing across OpenAI, Anthropic, Google, and Mistral. Reconciled LLM token costs with full price history, real-time change tracking, and agent-optimized APIs.",
  keywords: [
    "LLM pricing",
    "AI model pricing",
    "AI API cost",
    "GPT pricing",
    "Claude pricing",
    "LLM cost comparison",
    "token pricing",
    "AI token calculator",
    "LLM price tracker",
    "machine learning model cost",
    "OpenAI pricing",
    "Anthropic pricing",
  ],
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"),
  openGraph: {
    type: "website",
    siteName: "LLMRates",
    title: "LLMRates — AI Model Pricing API & Cost Comparison",
    description:
      "Compare AI model pricing across providers. Reconciled LLM token costs with full price history. Built for agents and developers.",
  },
  twitter: {
    card: "summary_large_image",
    title: "LLMRates — AI Model Pricing API & Cost Comparison",
    description:
      "Compare AI model pricing across providers. Multi-source verified LLM token costs with full price history and agent APIs.",
  },
  alternates: {
    canonical: "/",
  },
  robots: { index: true, follow: true },
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en">
      <body className={`${GeistSans.variable} ${GeistMono.variable} antialiased flex min-h-screen flex-col`}>
        <GoogleAnalytics />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: safeJsonLd(websiteJsonLd) }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: safeJsonLd(organizationJsonLd) }}
        />
        <Nav />
        <div className="flex-1">
          {children}
        </div>
        <Footer />
      </body>
    </html>
  )
}
