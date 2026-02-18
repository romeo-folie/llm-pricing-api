import type { Metadata } from "next"
import { Orbitron, Outfit } from "next/font/google"
import Nav from "@/components/layout/Nav"
import Footer from "@/components/layout/Footer"
import "./globals.css"

const orbitron = Orbitron({
  variable: "--font-orbitron-var",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700", "800", "900"],
  display: "swap",
})

const outfit = Outfit({
  variable: "--font-outfit-var",
  subsets: ["latin"],
  weight: ["300", "400", "500", "600", "700"],
  display: "swap",
})

export const metadata: Metadata = {
  title: {
    default: "LLMPrice — Reconciled LLM Token Pricing",
    template: "%s — LLMPrice",
  },
  description:
    "Reconciled, source-attributed LLM token pricing with full price history. Compare models, calculate costs, and track price changes in real time.",
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL ?? "https://llmprice.dev"),
  openGraph: {
    type: "website",
    siteName: "LLMPrice",
    title: "LLMPrice — Reconciled LLM Token Pricing",
    description:
      "Reconciled, source-attributed LLM token pricing with full price history. Built for agents and developers.",
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
      <body className={`${orbitron.variable} ${outfit.variable} antialiased flex min-h-screen flex-col`}>
        <Nav />
        <main className="flex-1">
          {children}
        </main>
        <Footer />
      </body>
    </html>
  )
}
