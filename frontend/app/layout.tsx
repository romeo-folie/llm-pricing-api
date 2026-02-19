import type { Metadata } from "next"
import { GeistSans } from "geist/font/sans"
import { GeistMono } from "geist/font/mono"
import Nav from "@/components/layout/Nav"
import Footer from "@/components/layout/Footer"
import "./globals.css"

export const metadata: Metadata = {
  title: {
    default: "LLMPrice — Reconciled LLM Token Pricing",
    template: "%s — LLMPrice",
  },
  description:
    "Reconciled, source-attributed LLM token pricing with full price history. Compare models, calculate costs, and track price changes in real time.",
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL || "https://llmprice.dev"),
  openGraph: {
    type: "website",
    siteName: "LLMPrice",
    title: "LLMPrice — Reconciled LLM Token Pricing",
    description:
      "Reconciled, source-attributed LLM token pricing with full price history. Built for agents and developers.",
  },
  twitter: {
    card: "summary_large_image",
    title: "LLMPrice — Reconciled LLM Token Pricing",
    description:
      "Multi-source verified LLM token pricing with full price history and agent-optimized APIs.",
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
        <Nav />
        <div className="flex-1">
          {children}
        </div>
        <Footer />
      </body>
    </html>
  )
}
