"use client"

import { useEffect, useRef, useState } from "react"
import type { FormEvent } from "react"
import { requestMagicLink } from "@/lib/signup"

interface Props {
  /**
   * Path to return to after the magic link is verified. The backend validates
   * this as a same-origin absolute path and drops anything else, so the agent's
   * approval survives the trip through the user's inbox.
   */
  next: string
  /** The agent's self-reported name, echoed back so the user knows what they are signing in for. */
  clientName?: string
}

/**
 * SignInForm is the inline sign-in step of the approval screen.
 *
 * It exists because the user arriving from a terminal often has no session yet.
 * Sending them to /signup/free and back would lose the user code, so the email
 * capture happens here and the code is carried through the magic link instead.
 */
export default function SignInForm({ next, clientName }: Props) {
  const [email, setEmail] = useState("")
  const [emailError, setEmailError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [sent, setSent] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    return () => {
      if (abortRef.current) abortRef.current.abort()
    }
  }, [])

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (isSubmitting) return

    const trimmed = email.trim()
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) {
      setEmailError("Enter a valid email address.")
      return
    }

    setEmailError(null)
    setError(null)
    setIsSubmitting(true)

    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    try {
      const result = await requestMagicLink(trimmed, controller.signal, next)
      if (controller.signal.aborted) return
      if (result.ok) {
        setSent(true)
      } else {
        setError(result.error.message)
      }
    } finally {
      if (!controller.signal.aborted) setIsSubmitting(false)
    }
  }

  if (sent) {
    return (
      <div className="activate-body">
        <div className="activate-icon" aria-hidden="true">
          <MailIcon />
        </div>
        <h2 className="activate-subheading">Check your email</h2>
        <p className="activate-copy">
          We sent a sign-in link to <strong className="activate-strong">{email.trim()}</strong>. Open
          it in this browser and you will come straight back here to finish approving the request.
        </p>
        <p className="activate-footnote">
          The code stays valid while you fetch the email. If it lapses you can start again from your
          terminal.
        </p>
      </div>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="activate-body">
      <p className="activate-copy">
        {clientName ? (
          <>
            <strong className="activate-strong">{clientName}</strong> asked to use your account, so
            you need to sign in before you can decide.
          </>
        ) : (
          <>Sign in to review this request.</>
        )}
      </p>

      <div className="signup-field">
        <label htmlFor="activate-email" className="signup-label">
          Email
        </label>
        <input
          id="activate-email"
          type="email"
          required
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value)
            if (emailError) setEmailError(null)
            if (error) setError(null)
          }}
          disabled={isSubmitting}
          className="signup-input"
          aria-invalid={emailError || error ? "true" : undefined}
          aria-describedby={emailError ? "activate-email-error" : error ? "activate-error" : undefined}
        />
      </div>

      {emailError && (
        <p id="activate-email-error" className="signup-error" role="alert">
          {emailError}
        </p>
      )}
      {error && (
        <p id="activate-error" className="signup-error" role="alert">
          {error}
        </p>
      )}

      <button
        type="submit"
        disabled={isSubmitting || !email.trim()}
        className="signup-submit"
        aria-busy={isSubmitting}
      >
        {isSubmitting ? <span className="signup-spinner" aria-hidden="true" /> : "Send sign-in link"}
      </button>
    </form>
  )
}

function MailIcon() {
  return (
    <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
      <rect x="3" y="6" width="22" height="16" rx="2.5" stroke="currentColor" strokeWidth="1.75" />
      <path
        d="M4.5 8.5 14 15l9.5-6.5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
