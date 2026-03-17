"use client"

import { useEffect, useRef, useState } from "react"
import { issueKey, type IdentityResponse } from "@/lib/signup"

interface Props {
  identity: IdentityResponse
}

type KeyState =
  | { phase: "loading" }
  | { phase: "ready"; plaintext: string }
  | { phase: "error"; message: string }

export default function VerifiedKeyPanel({ identity }: Props) {
  const [keyState, setKeyState] = useState<KeyState>(
    identity.has_active_key ? { phase: "ready", plaintext: "" } : { phase: "loading" },
  )
  const [copied, setCopied] = useState(false)
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const hasFired = useRef(false)

  // Auto-issue key on mount for new identities.
  // Skipped if a key already exists — user must use the manage page to regenerate.
  useEffect(() => {
    if (identity.has_active_key || hasFired.current) return
    hasFired.current = true

    let cancelled = false
    const controller = new AbortController()

    ;(async () => {
      const result = await issueKey(controller.signal)
      if (cancelled) return
      if (result.ok) {
        setKeyState({ phase: "ready", plaintext: result.key.plaintext })
      } else {
        if (result.error.code === "aborted") return
        setKeyState({ phase: "error", message: result.error.message })
      }
    })()

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [identity.has_active_key])

  useEffect(() => {
    return () => {
      if (copyTimer.current) clearTimeout(copyTimer.current)
    }
  }, [])

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      return
    }
    setCopied(true)
    if (copyTimer.current) clearTimeout(copyTimer.current)
    copyTimer.current = setTimeout(() => setCopied(false), 2000)
  }

  // Existing user landing (key already issued — show manage link, not the plaintext)
  if (identity.has_active_key) {
    return (
      <div className="verified-card">
        <div className="verified-icon verified-icon-success" aria-hidden="true">
          <CheckCircleIcon />
        </div>
        <h1 className="verified-heading">Email verified</h1>
        <p className="verified-sub">
          You already have an active API key. Manage it from the signup page.
        </p>
        <a href="/signup/free" className="verified-btn">
          Manage your key
        </a>
        <a href="/docs" className="verified-link">
          View API docs →
        </a>
      </div>
    )
  }

  if (keyState.phase === "loading") {
    return (
      <div className="verified-loading" role="status" aria-label="Issuing key">
        <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
        <p className="verified-loading-text">Issuing your API key…</p>
      </div>
    )
  }

  if (keyState.phase === "error") {
    return (
      <div className="verified-card">
        <div className="verified-icon verified-icon-error" aria-hidden="true">
          <ErrorIcon />
        </div>
        <h1 className="verified-heading">Something went wrong</h1>
        <p className="verified-sub">{keyState.message}</p>
        <a href="/signup/free" className="verified-btn">
          Back to signup
        </a>
      </div>
    )
  }

  // New key issued — show it once with copy button
  const { plaintext } = keyState
  const isHidden = !plaintext

  return (
    <div className="verified-card verified-card-key">
      {/* Identity header */}
      <div className="verified-identity">
        <span className="verified-email">{identity.email}</span>
        {identity.email_verified && (
          <span className="verified-badge" title="Email verified">
            <CheckIcon />
            verified
          </span>
        )}
      </div>

      {/* Success heading */}
      <div className="verified-success-icon" aria-hidden="true">
        <CheckCircleIcon />
      </div>
      <h1 className="verified-heading">You&apos;re in.</h1>
      <p className="verified-sub">
        Your free API key is ready. Use it in the{" "}
        <code className="verified-code">Authorization: Bearer</code> header.
        <br />
        <span className="verified-sub-dim">100 requests / day · no credit card required</span>
      </p>

      {/* Key display */}
      {!isHidden && (
        <>
          <div className="verified-key-display">
            <code className="verified-key-value" aria-label="API key">
              {plaintext}
            </code>
            <button
              type="button"
              onClick={() => void handleCopy(plaintext)}
              className={`verified-copy-btn ${copied ? "verified-copy-btn-copied" : ""}`}
              aria-label={copied ? "Key copied" : "Copy API key"}
            >
              {copied ? (
                <>
                  <CheckIcon />
                  <span>Copied</span>
                </>
              ) : (
                <>
                  <CopyIcon />
                  <span>Copy key</span>
                </>
              )}
            </button>
          </div>

          <p className="verified-once-warning">
            ⚠ Copy your key now — it won&apos;t be shown again after you leave this page.
          </p>
        </>
      )}

      {/* Footer links */}
      <div className="verified-footer">
        <a href="/docs" className="verified-btn verified-btn-outline">
          View API docs
        </a>
        <a href="/" className="verified-link">
          Explore pricing →
        </a>
      </div>
    </div>
  )
}

// ── Icons ─────────────────────────────────────────────────────────────────────

function CheckCircleIcon() {
  return (
    <svg width="40" height="40" viewBox="0 0 40 40" fill="none" aria-hidden="true">
      <circle cx="20" cy="20" r="18" stroke="currentColor" strokeWidth="2" />
      <path
        d="M11 20l6 6 12-13"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function CopyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <rect x="4" y="4" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M2 10V3a1 1 0 011-1h7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <path
        d="M2.5 7l3.5 3.5 5.5-6"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function ErrorIcon() {
  return (
    <svg width="32" height="32" viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <circle cx="16" cy="16" r="14" stroke="currentColor" strokeWidth="2" />
      <path d="M16 9v8M16 21v2" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
    </svg>
  )
}
