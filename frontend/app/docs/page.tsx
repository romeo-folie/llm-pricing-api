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
        /* Hide the two central decorative rail lines — they sit in the middle of the
           Scalar content area and are not appropriate for the full-width docs layout. */
        body::before, body::after { display: none !important; }

        /* Extend the Scalar sidebar border to the very top of the viewport.
           The aside starts below the nav (top: 58px). A fixed pseudo-element
           fills the gap from top: 0 to the sidebar top with the same border colour. */
        .t-doc__sidebar::before {
          content: "";
          position: fixed;
          top: 0;
          left: 0;
          width: 1px;
          height: 58px;
          background-color: var(--scalar-border-color, #DDD7D0);
          z-index: 49;
          pointer-events: none;
        }
        @media (prefers-color-scheme: dark) {
          .t-doc__sidebar::before {
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
