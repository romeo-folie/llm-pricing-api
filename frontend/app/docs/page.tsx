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

        /* Extend the Scalar sidebar right-border to the top of the viewport.
           Scalar's sidebar is 288px wide and starts 58px from the top (below the nav).
           This fixed element fills the 58px gap above it. */
        #docs-sidebar-extender {
          position: fixed;
          top: 0;
          left: calc(288px - 0.5px); /* center-align with sidebar's subpixel border */
          width: 1px;
          height: 58px;
          background-color: #DDD7D0;
          z-index: 51; /* above header z-50 so the line passes through it */
          pointer-events: none;
        }
        @media (prefers-color-scheme: dark) {
          #docs-sidebar-extender {
            background-color: #413F3C;
          }
        }
      `}</style>
      {/* Gap-fill: extends sidebar border from viewport top to where Scalar's aside starts */}
      <div id="docs-sidebar-extender" aria-hidden="true" />
      <main>
        <ApiReference specUrl="/openapi.json" />
      </main>
    </>
  )
}
