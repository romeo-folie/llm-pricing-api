"use client"

import { useEffect, useRef, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { getIdentity, type IdentityResponse, type SignupStep } from "@/lib/signup"
import RequestLinkForm from "@/components/signup/RequestLinkForm"
import SentConfirmation from "@/components/signup/SentConfirmation"
import KeyPanel from "@/components/signup/KeyPanel"

/**
 * SignupFlow orchestrates the state machine:
 *
 *  request  →  sent  (after form submission)
 *  ↓ (on page load with ?verified=1 and valid session)
 *  verified  →  already-issued (if key exists)
 *
 * The verify callback (GET /auth/signup/verify?token=...) is handled entirely
 * server-side. The backend marks the identity verified, creates a session
 * cookie, and redirects to /signup/free?verified=1. This component detects
 * that param and immediately calls /auth/signup/me to retrieve identity state.
 */
export default function SignupFlow() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const verified = searchParams.get("verified") === "1"

  const [step, setStep] = useState<SignupStep>(verified ? "verifying" : "request")
  const [sentEmail, setSentEmail] = useState("")
  const [sentAt, setSentAt] = useState<number | null>(null)
  const [identity, setIdentity] = useState<IdentityResponse | null>(null)
  const [verifyError, setVerifyError] = useState<string | null>(null)

  const fetchedRef = useRef(false)

  // When `verified` becomes true after mount (e.g. navigation with ?verified=1),
  // transition to the verifying step so the identity fetch runs.
  // Conversely, when `verified` becomes false (e.g. after router.replace removes
  // the query param), unblock the verifying→request transition so "Try again"
  // doesn't leave the UI stuck on the spinner.
  useEffect(() => {
    if (verified && step === "request") {
      setStep("verifying")
    }
    if (!verified && step === "verifying") {
      setStep("request")
    }
  }, [verified, step])

  useEffect(() => {
    if (!verified || fetchedRef.current) return
    fetchedRef.current = true

    const controller = new AbortController()
    ;(async () => {
      try {
        const result = await getIdentity(controller.signal)
        if (result.ok) {
          setIdentity(result.identity)
          setStep(result.identity.has_active_key ? "already-issued" : "verified")
        } else {
          if (controller.signal.aborted) return
          setVerifyError(
            "We couldn't confirm your session. The link may have expired — try again."
          )
          setStep("error")
        }
      } catch (err) {
        if ((err as Error).name === "AbortError") return
        throw err
      }
    })()

    return () => controller.abort()
  }, [verified])

  // ── Render ────────────────────────────────────────────────────────────────

  if (step === "verifying") {
    return (
      <div className="signup-flow-center" role="status" aria-label="Verifying">
        <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
        <p className="signup-flow-status">Verifying your link…</p>
      </div>
    )
  }

  if (step === "error") {
    return (
      <div className="signup-flow-center">
        <p className="signup-error" role="alert">
          {verifyError ?? "Something went wrong."}
        </p>
        <button
          className="signup-link-btn"
          onClick={() => {
            fetchedRef.current = false
            setVerifyError(null)
            // Clear the query param first so the verified→verifying effect
            // doesn't race with the step reset.
            router.replace("/signup/free")
            setStep("request")
            // If verified is still true when React re-renders, the effect
            // will briefly flip to "verifying", but the !verified guard
            // resets back to "request" once the URL update lands.
          }}
        >
          Try again
        </button>
      </div>
    )
  }

  if ((step === "verified" || step === "already-issued") && identity) {
    return <KeyPanel identity={identity} />
  }

  if (step === "sent") {
    const COOLDOWN_DURATION = 60_000
    const elapsed = sentAt ? Date.now() - sentAt : COOLDOWN_DURATION
    const initialCooldownMs = Math.max(0, COOLDOWN_DURATION - elapsed)

    return (
      <SentConfirmation
        email={sentEmail}
        initialCooldownMs={initialCooldownMs}
        onBack={() => {
          setSentEmail("")
          setSentAt(null)
          setStep("request")
        }}
      />
    )
  }

  // Default: request step
  return (
    <RequestLinkForm
      onSent={(email) => {
        setSentEmail(email)
        setSentAt(Date.now())
        setStep("sent")
      }}
    />
  )
}
