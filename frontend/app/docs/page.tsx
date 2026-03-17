import type { Metadata } from "next"
import ApiReference from "@/components/docs/ApiReference"

export const metadata: Metadata = {
  title: "API Reference",
  description:
    "Full REST API reference for LLMRates. Explore endpoints for LLM model pricing, price history, provider comparison, and agent-optimized queries.",
  alternates: { canonical: "/docs" },
}

export default function DocsPage() {
  return (
    <>
      <main>
        <ApiReference specUrl="/openapi.json" />
      </main>
    </>
  )
}
