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
    default: "LLMRates | LLM Pricing, Token Costs & Model Comparison",
    template: "%s | LLMRates",
  },
  description:
    "Compare LLM pricing and AI API costs across providers. Explore current input and output token prices, price history, cost calculators, and developer APIs.",
  keywords: [
    "LLM pricing",
    "LLM prices",
    "LLM costs",
    "LLM pricing comparison",
    "LLM cost comparison",
    "AI model pricing",
    "AI model costs",
    "AI API pricing",
    "AI API costs",
    "token pricing",
    "token prices",
    "token costs",
    "price per million tokens",
    "LLM price calculator",
    "LLM cost calculator",
    "AI API cost calculator",
    "AI token cost calculator",
    "LLM price tracker",
    "AI model price tracker",
  ],
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"),
  openGraph: {
    type: "website",
    siteName: "LLMRates",
    title: "LLMRates | LLM Pricing, Token Costs & Model Comparison",
    description:
      "Compare LLM pricing and AI API costs across providers with current token prices, full price history, and developer APIs.",
    images: [
      {
        url: `${SITE_URL}/og-image.png`,
        width: 1188,
        height: 613,
        alt: "LLMRates — Compare AI Model Pricing Across Providers",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "LLMRates | LLM Pricing, Token Costs & Model Comparison",
    description:
      "Compare AI model pricing, token costs, and price history across LLM providers.",
    images: [`${SITE_URL}/og-image.png`],
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
