import type { Metadata } from "next"
import { Suspense } from "react"
import ActivateFlow from "./ActivateFlow"

export const metadata: Metadata = {
  title: "Approve agent access — LLMRates",
  description: "Approve or deny an agent's request for an API key on your account.",
  // A consent screen is per-user and per-request; it must never be indexed.
  robots: { index: false, follow: false },
}

export default function ActivatePage() {
  return (
    <main className="activate-page">
      <Suspense
        fallback={
          <div className="activate-card" role="status" aria-label="Loading">
            <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
          </div>
        }
      >
        <ActivateFlow />
      </Suspense>
    </main>
  )
}
