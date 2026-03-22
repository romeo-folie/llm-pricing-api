"use client"

import { useEffect, useRef, useState } from "react"
import { requestMagicLink, type ApiError } from "@/lib/signup"
import { useCooldown } from "@/hooks/useCooldown"

interface Props {
  email: string
  initialCooldownMs?: number
  onBack: () => void
}

export default function SentConfirmation({ email, initialCooldownMs, onBack }: Props) {
  const [resent, setResent] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const [isResending, setIsResending] = useState(false)
  const { cooldownMs, startCooldown } = useCooldown()
  const abortRef = useRef<AbortController | null>(null)
  const initializedRef = useRef(false)

  useEffect(() => {
    if (!initializedRef.current && initialCooldownMs && initialCooldownMs > 0) {
      initializedRef.current = true
      startCooldown(initialCooldownMs)
    }
  }, [initialCooldownMs, startCooldown])

  useEffect(() => {
    return () => {
      if (abortRef.current) abortRef.current.abort()
    }
  }, [])

  const handleResend = async () => {
    if (isResending || cooldownMs > 0) return
    setError(null)
    setResent(false)
    setIsResending(true)

    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    try {
      const result = await requestMagicLink(email, controller.signal)
      if (controller.signal.aborted) return
      if (result.ok) {
        setResent(true)
        startCooldown(60_000)
      } else {
        setError(result.error)
        if (result.error.retryAfterMs) {
          startCooldown(result.error.retryAfterMs)
        }
      }
    } finally {
      if (!controller.signal.aborted) {
        setIsResending(false)
      }
    }
  }

  return (
    <div className="signup-sent animate-reveal-card">
      <div className="signup-sent-icon" aria-hidden="true">
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
          <rect width="48" height="48" rx="8" fill="var(--accentLt)" />
          <path
            d="M8 16l16 11 16-11"
            stroke="var(--accent)"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <rect
            x="8"
            y="14"
            width="32"
            height="22"
            rx="3"
            stroke="var(--accent)"
            strokeWidth="2.5"
          />
        </svg>
      </div>

      <h2 className="signup-sent-heading">Check your inbox</h2>
      <p className="signup-sent-body">
        We sent a magic link to{" "}
        <strong className="signup-sent-email">{email}</strong>.
        <br />
        It expires shortly. No code to copy — just click the link.
      </p>

      {resent && (
        <p className="signup-sent-notice" role="status">
          Link resent ✓
        </p>
      )}
      {error && (
        <p className="signup-error" role="alert">
          {error.message}
        </p>
      )}

      <div className="signup-sent-actions">
        <button
          onClick={handleResend}
          disabled={isResending || cooldownMs > 0}
          className="signup-link-btn"
          aria-busy={isResending}
        >
          {isResending ? "Sending…" : cooldownMs > 0 ? `Resend in ${Math.ceil(cooldownMs / 1000)}s` : "Resend link"}
        </button>
        <span className="signup-sent-sep" aria-hidden="true">·</span>
        <button onClick={onBack} className="signup-link-btn">
          Use different email
        </button>
      </div>
    </div>
  )
}
