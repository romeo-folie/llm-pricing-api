"use client"

import { useEffect, useRef, useState } from "react"
import { issueKey, regenerateKey, type IdentityResponse, type ApiError } from "@/lib/signup"

interface Props {
  identity: IdentityResponse
  /**
   * When the verify callback already issued the key (future extension),
   * pass it here to skip the issue-key call.
   */
  initialKey?: string
}

type KeyState =
  | { phase: "loading" }
  | { phase: "revealing"; plaintext: string }
  | { phase: "hidden" }
  | { phase: "regenerating" }
  | { phase: "error"; error: ApiError }

export default function KeyPanel({ identity, initialKey }: Props) {
  const [keyState, setKeyState] = useState<KeyState>(
    initialKey
      ? { phase: "revealing", plaintext: initialKey }
      : identity.has_active_key
      ? { phase: "hidden" }
      : { phase: "loading" }
  )
  const [copied, setCopied] = useState(false)
  const [isRegenerating, setIsRegenerating] = useState(false)
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const regenAbortRef = useRef<AbortController | null>(null)

  // Auto-issue key on mount if identity has no key yet.
  // Guarded by hasFired ref to ensure idempotency across strict-mode double-mounts.
  const hasFired = useRef(false)
  useEffect(() => {
    if (identity.has_active_key || initialKey || hasFired.current) return
    hasFired.current = true

    let cancelled = false
    const controller = new AbortController()

    ;(async () => {
      const result = await issueKey(controller.signal)
      if (cancelled) return
      if (result.ok) {
        setKeyState({ phase: "revealing", plaintext: result.key.plaintext })
      } else {
        if (result.error.code === "aborted") return
        setKeyState({ phase: "error", error: result.error })
      }
    })()

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [identity.has_active_key, initialKey])

  // Clean up timers and in-flight requests on unmount.
  useEffect(() => {
    return () => {
      if (copyTimer.current) clearTimeout(copyTimer.current)
      if (regenAbortRef.current) regenAbortRef.current.abort()
    }
  }, [])

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard API may be unavailable (e.g. non-HTTPS, permission denied).
      return
    }
    setCopied(true)
    if (copyTimer.current) clearTimeout(copyTimer.current)
    copyTimer.current = setTimeout(() => setCopied(false), 2000)
  }

  const handleRegenerate = async () => {
    if (isRegenerating) return
    const confirmed = confirm(
      "Regenerating your key will immediately revoke the old one. Any applications using it will stop working. Continue?"
    )
    if (!confirmed) return

    setKeyState({ phase: "regenerating" })
    setIsRegenerating(true)

    if (regenAbortRef.current) regenAbortRef.current.abort()
    const controller = new AbortController()
    regenAbortRef.current = controller

    try {
      const result = await regenerateKey(controller.signal)
      if (controller.signal.aborted) return
      if (result.ok) {
        setKeyState({ phase: "revealing", plaintext: result.key.plaintext })
      } else {
        setKeyState({ phase: "error", error: result.error })
      }
    } finally {
      if (!controller.signal.aborted) {
        setIsRegenerating(false)
      }
    }
  }

  const maskedKey = "sk-llmr-••••••••••••••••••••••••••••••"

  return (
    <div className="key-panel animate-reveal-card">
      <div className="key-panel-header">
        <div className="key-panel-identity">
          <span className="key-panel-email">{identity.email}</span>
          {identity.email_verified && (
            <span className="key-panel-badge" title="Email verified">
              ✓ verified
            </span>
          )}
        </div>
      </div>

      <div className="key-panel-section">
        <h2 className="key-panel-heading">Your API key</h2>
        <p className="key-panel-desc">
          Use this key in the{" "}
          <code className="key-panel-code">Authorization: Bearer &lt;key&gt;</code> header.
          Free tier: 100 requests/day.
        </p>

        {keyState.phase === "loading" && (
          <div className="key-panel-loading" role="status" aria-label="Issuing key">
            <span className="signup-spinner" aria-hidden="true" />
            <span>Issuing your key…</span>
          </div>
        )}

        {keyState.phase === "regenerating" && (
          <div className="key-panel-loading" role="status" aria-label="Regenerating key">
            <span className="signup-spinner" aria-hidden="true" />
            <span>Revoking old key and issuing new one…</span>
          </div>
        )}

        {(keyState.phase === "revealing" || keyState.phase === "hidden") && (
          <div className="key-display">
            <code className="key-display-value">
              {keyState.phase === "revealing" ? keyState.plaintext : maskedKey}
            </code>
            <div className="key-display-actions">
              {keyState.phase === "revealing" && (
                <button
                  onClick={() => handleCopy(keyState.plaintext)}
                  className="key-btn"
                  aria-label={copied ? "Copied" : "Copy key"}
                >
                  {copied ? (
                    <>
                      <CheckIcon />
                      Copied
                    </>
                  ) : (
                    <>
                      <CopyIcon />
                      Copy
                    </>
                  )}
                </button>
              )}
            </div>
          </div>
        )}

        {keyState.phase === "revealing" && (
          <p className="key-panel-once-notice" role="status">
            ⚠ Copy your key now — it won&apos;t be shown again after you leave this page.
          </p>
        )}

        {keyState.phase === "error" && (
          <p className="signup-error" role="alert">
            {keyState.error.message}
          </p>
        )}
      </div>

      <div className="key-panel-footer">
        <button
          onClick={handleRegenerate}
          disabled={keyState.phase === "loading" || keyState.phase === "regenerating"}
          className="signup-link-btn key-regen-btn"
          aria-busy={isRegenerating}
        >
          Regenerate key
        </button>
        <span className="signup-sent-sep" aria-hidden="true">·</span>
        <a href="/docs" className="signup-link-btn">
          View docs
        </a>
      </div>
    </div>
  )
}

function CopyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <rect x="4" y="4" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="M2 10V3a1 1 0 011-1h7"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
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
