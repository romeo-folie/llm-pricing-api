import type { Metadata } from "next"
import { Suspense } from "react"
import VerifiedFlow from "./VerifiedFlow"

export const metadata: Metadata = {
  title: "Email Verified — LLMRates",
  description: "Your email has been verified. Grab your API key.",
  robots: { index: false },
}

export default function VerifiedPage() {
  return (
    <div className="verified-page">
      <Suspense
        fallback={
          <div className="verified-loading" role="status" aria-label="Loading">
            <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
          </div>
        }
      >
        <VerifiedFlow />
      </Suspense>
    </div>
  )
}
