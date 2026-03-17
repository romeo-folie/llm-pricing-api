import type { Metadata } from "next"
import ApiReference from "@/components/docs/ApiReference"
import DocsPageStyles from "@/components/docs/DocsPageStyles"

export const metadata: Metadata = {
  title: "API Reference",
  description:
    "Full REST API reference for LLMRates. Explore endpoints for LLM model pricing, price history, provider comparison, and agent-optimized queries.",
  alternates: { canonical: "/docs" },
}

export default function DocsPage() {
  return (
    <>
      {/* Hide the global decorative rail lines — they interfere with Scalar's full-width layout */}
      <style>{`body::before, body::after { display: none !important; }`}</style>
      {/* Client component that adds/removes data-page="docs" on <html> for CSS targeting */}
      <DocsPageStyles />
      <main>
        <ApiReference specUrl="/openapi.json" />
      </main>
    </>
  )
}
