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
 *  ↓ (on page load with ?verified=1 — legacy; new flow redirects to /signup/verified)
 *  verified  →  already-issued (if key exists)
 *
 * Error states surfaced via ?error= query param (set by the backend redirect):
 *   link-used     — token was already consumed
 *   link-expired  — token TTL elapsed
 *   invalid-link  — token not found / malformed
 *   server-error  — unexpected backend error
 *
 * The verify callback (GET /auth/signup/verify?token=...) is handled entirely
 * server-side. The backend marks the identity verified, creates a session
 * cookie, and redirects to /signup/verified. This component still handles the
 * legacy ?verified=1 param for users with cached/old magic-link emails.
 */

const ERROR_MESSAGES: Record<string, string> = {
  "link-used":
    "This sign-in link has already been used. Request a new one below.",
  "link-expired":
    "This sign-in link has expired. Request a fresh one — it only takes a second.",
  "invalid-link":
    "This link isn't valid. It may have been copied incorrectly. Try requesting a new one.",
  "server-error":
    "Something went wrong on our end. Please try again.",
}

export default function SignupFlow() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const verified = searchParams.get("verified") === "1"
  const errorCode = searchParams.get("error")

  // If the backend redirected here with ?error=, start in the error step
  // and show the appropriate message. Clear the query param immediately so
  // a reload doesn't re-show the error.
  const initialStep: SignupStep = errorCode
    ? "error"
    : verified
      ? "verifying"
      : "request"

  const [step, setStep] = useState<SignupStep>(initialStep)
  const [sentEmail, setSentEmail] = useState("")
  const [sentAt, setSentAt] = useState<number | null>(null)
  const [identity, setIdentity] = useState<IdentityResponse | null>(null)
  const [verifyError, setVerifyError] = useState<string | null>(
    errorCode ? (ERROR_MESSAGES[errorCode] ?? ERROR_MESSAGES["server-error"] ?? null) : null
  )

  const fetchedRef = useRef(false)

  // Strip the ?error= / ?verified= param from the URL so a reload doesn't
  // re-trigger the same state. Replace (not push) to avoid adding history entries.
  useEffect(() => {
    if (errorCode || verified) {
      router.replace("/signup/free", { scroll: false })
    }
    // Only run on mount — intentionally omitting router from deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
            setStep("request")
          }}
        >
          Request a new link
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
