"use client"

import { useEffect, useRef, useState } from "react"
import type { FormEvent } from "react"
import { requestMagicLink, type ApiError } from "@/lib/signup"
import { useCooldown } from "@/hooks/useCooldown"

interface Props {
  onSent: (email: string) => void
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export default function RequestLinkForm({ onSent }: Props) {
  const [email, setEmail] = useState("")
  const [error, setError] = useState<ApiError | null>(null)
  const [emailError, setEmailError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const { cooldownMs, startCooldown } = useCooldown()
  const abortRef = useRef<AbortController | null>(null)

  // Abort in-flight request on unmount.
  useEffect(() => {
    return () => {
      if (abortRef.current) abortRef.current.abort()
    }
  }, [])

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (isSubmitting || cooldownMs > 0) return

    const trimmed = email.trim()
    if (!EMAIL_RE.test(trimmed)) {
      setEmailError("Please enter a valid email address.")
      return
    }

    setEmailError(null)
    setError(null)
    setIsSubmitting(true)

    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    try {
      const result = await requestMagicLink(email.trim(), controller.signal)
      if (controller.signal.aborted) return
      if (result.ok) {
        onSent(email.trim())
      } else {
        setError(result.error)
        if (result.error.retryAfterMs) {
          startCooldown(result.error.retryAfterMs)
        }
      }
    } finally {
      if (!controller.signal.aborted) {
        setIsSubmitting(false)
      }
    }
  }

  const cooldownSec = Math.ceil(cooldownMs / 1000)
  const isBlocked = isSubmitting || cooldownMs > 0

  return (
    <form onSubmit={handleSubmit} className="signup-form animate-reveal-card">
      <div className="signup-field">
        <label htmlFor="signup-email" className="signup-label">
          Work or personal email
        </label>
        <input
          id="signup-email"
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
          disabled={isBlocked}
          className="signup-input"
          aria-describedby={emailError ? "signup-email-error" : error ? "signup-error" : undefined}
          aria-invalid={emailError || error ? "true" : undefined}
        />
      </div>

      {emailError && (
        <p id="signup-email-error" className="signup-error" role="alert">
          {emailError}
        </p>
      )}

      {error && (
        <p id="signup-error" className="signup-error" role="alert">
          {error.message}
        </p>
      )}

      <button
        type="submit"
        disabled={isBlocked || !email.trim()}
        className="signup-submit"
        aria-busy={isSubmitting}
      >
        {isSubmitting ? (
          <span className="signup-spinner" aria-hidden="true" />
        ) : cooldownMs > 0 ? (
          <>Resend in {cooldownSec}s</>
        ) : (
          "Send magic link"
        )}
      </button>

      <p className="signup-footnote">
        No password. No credit card. One link — you&apos;re in.
      </p>
    </form>
  )
}
