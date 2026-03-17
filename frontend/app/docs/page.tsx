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
      <style>{`
        /* Hide the two central decorative rail lines on the docs page. */
        body::before, body::after { display: none !important; }

        /* Draw a line through the nav header that aligns with the Scalar sidebar
           right-border (at x=288px). Uses the nav ::after pseudo so it renders
           inside the nav stacking context — guaranteed above the nav background
           regardless of z-index layering. */
        header::after {
          content: "";
          position: absolute;
          top: 0;
          left: 288px;
          width: 1px;
          height: 100%;
          background-color: #DDD7D0;
          pointer-events: none;
        }
        @media (prefers-color-scheme: dark) {
          header::after {
            background-color: #413F3C;
          }
        }
      `}</style>
      <main>
        <ApiReference specUrl="/openapi.json" />
      </main>
    </>
  )
}
