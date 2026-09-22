"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "next/navigation"
import {
  decideAgentGrant,
  getAgentGrant,
  isValidUserCode,
  normalizeUserCode,
  type AgentDecision,
  type AgentGrant,
} from "@/lib/agent"
import SignInForm from "./SignInForm"
import "./activate.css"

/**
 * The approval screen for the agent device-grant flow.
 *
 * Design intent: this is a consent decision, so the screen's job is to make the
 * two things a user must verify unmissable, namely which agent is asking and
 * which account it would act as. The user code is shown large and monospaced so
 * it can be compared against the terminal that started the request: that
 * comparison is the defence against a code being relayed by someone else.
 *
 * The API key deliberately never appears here. It is collected by the agent, so
 * this page has nothing to copy and nothing to leak.
 */

type Phase =
  | { kind: "loading" }
  | { kind: "sign-in" }
  | { kind: "review"; grant: AgentGrant }
  | { kind: "deciding"; grant: AgentGrant }
  | { kind: "done"; decision: AgentDecision; clientName: string }
  | { kind: "closed"; title: string; message: string }
  | { kind: "error"; message: string }

const CLOSED_EXPIRED: Phase = {
  kind: "closed",
  title: "This code has expired",
  message:
    "Codes are only valid for a few minutes. Ask the agent to start the request again and it will give you a fresh one.",
}

const CLOSED_HANDLED: Phase = {
  kind: "closed",
  title: "This request was already handled",
  message:
    "It has either been used already or decided. If the agent is still waiting, ask it to start a new request.",
}

/**
 * Where the in-progress code is stashed across the sign-in round trip.
 *
 * The code is deliberately NOT carried in the `next` URL. `request-link` is
 * unauthenticated and emails an arbitrary address, so a code in `next` would let
 * an attacker send a victim a genuine llmrates.live email that opens the
 * approval screen for the *attacker's* grant, which is exactly the code-relay
 * the approval screen exists to prevent. Keeping it in this browser only means
 * the user must have obtained the code themselves.
 */
const CODE_STORAGE_KEY = "llmrates.activate.code"

export default function ActivateFlow() {
  const searchParams = useSearchParams()

  const [code, setCode] = useState<string | null>(null)
  const [codeResolved, setCodeResolved] = useState(false)
  const [codeInput, setCodeInput] = useState("")
  const [codeError, setCodeError] = useState<string | null>(null)
  const [phase, setPhase] = useState<Phase>({ kind: "loading" })
  const [now, setNow] = useState(() => Date.now())
  const headingRef = useRef<HTMLHeadingElement | null>(null)
  const resolvedRef = useRef(false)

  // The sign-in round trip returns to a bare /activate, so `next` never needs to
  // carry anything.
  const nextPath = "/activate"

  // Resolve the code once: from the URL if the user came straight from their
  // terminal, otherwise from whatever the pre-sign-in step stashed.
  useEffect(() => {
    if (resolvedRef.current) return
    resolvedRef.current = true

    const fromUrl = normalizeUserCode(searchParams.get("code") ?? "")
    if (isValidUserCode(fromUrl)) {
      stashCode(fromUrl)
      setCode(fromUrl)
      setCodeResolved(true)
      return
    }

    const stored = readStashedCode()
    if (stored) setCode(stored)
    setCodeResolved(true)
  }, [searchParams])

  // Load the grant once a code is known. The code is fixed for the life of the
  // page, so this must not re-run on unrelated state changes.
  useEffect(() => {
    if (!code) return

    const controller = new AbortController()
    let cancelled = false

    ;(async () => {
      const result = await getAgentGrant(code, controller.signal)
      if (cancelled) return

      if (result.ok) {
        switch (result.data.status) {
          case "pending":
            setPhase({ kind: "review", grant: result.data })
            break
          case "expired":
            setPhase(CLOSED_EXPIRED)
            break
          case "denied":
            setPhase({
              kind: "closed",
              title: "You denied this request",
              message: "Nothing was shared. The agent has been told you declined.",
            })
            break
          case "approved":
          case "redeemed":
            setPhase(CLOSED_HANDLED)
            break
          default:
            setPhase({ kind: "review", grant: result.data })
        }
        return
      }

      switch (result.code) {
        case "needs_session":
          setPhase({ kind: "sign-in" })
          break
        case "expired":
          setPhase(CLOSED_EXPIRED)
          break
        case "unknown_code":
          setPhase({
            kind: "closed",
            title: "We do not recognise that code",
            message:
              "It may have expired, already been used, or been mistyped. Ask the agent for a new one.",
          })
          break
        case "aborted":
          break
        default:
          setPhase({ kind: "error", message: result.message })
      }
    })()

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [code])

  // Announce each new decision point to screen readers.
  useEffect(() => {
    if (phase.kind === "loading") return
    headingRef.current?.focus()
  }, [phase.kind])

  const isDecidable = phase.kind === "review" || phase.kind === "deciding"
  const activeGrant = isDecidable ? phase.grant : null
  const expiresAt = activeGrant ? new Date(activeGrant.expires_at).getTime() : 0
  const remainingMs = expiresAt > 0 ? expiresAt - now : 0

  // Tick only while a countdown is on screen.
  useEffect(() => {
    if (!isDecidable) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [isDecidable])

  // Close the decision off when the window lapses, rather than letting the user
  // approve something the server will refuse.
  useEffect(() => {
    if (phase.kind === "review" && expiresAt > 0 && remainingMs <= 0) {
      setPhase(CLOSED_EXPIRED)
    }
  }, [phase.kind, expiresAt, remainingMs])

  const decide = useCallback(
    async (decision: AgentDecision) => {
      if (phase.kind !== "review" || !code) return
      const { grant } = phase
      setPhase({ kind: "deciding", grant })

      const controller = new AbortController()
      const result = await decideAgentGrant(code, decision, controller.signal)

      if (result.ok) {
        setPhase({ kind: "done", decision, clientName: result.data.client_name || grant.client_name })
        return
      }

      switch (result.code) {
        case "aborted":
          return
        case "needs_session":
          setPhase({ kind: "sign-in" })
          return
        case "expired":
          setPhase(CLOSED_EXPIRED)
          return
        case "unknown_code":
        case "already_used":
          setPhase(CLOSED_HANDLED)
          return
        case "rate_limited":
          setPhase({ kind: "error", message: result.message })
          return
        default:
          setPhase({ kind: "error", message: result.message })
      }
    },
    [phase, code]
  )

  return (
    <div className="activate-card">
      <div className="activate-brand">
        <a href="/" className="activate-logo">
          LLM<span className="activate-logo-accent">Rates</span>
        </a>
        <span className="activate-eyebrow">Agent authorization</span>
      </div>

      {renderPhase()}
    </div>
  )

  function renderPhase() {
    // No code yet: either still resolving it, or the visitor arrived without one
    // (for example after the email round trip in a different browser). The code
    // is what authorises the approval, so it must come from the user rather than
    // from a link someone sent them.
    if (!codeResolved) return renderLoading()
    if (!code) return renderCodeEntry()

    switch (phase.kind) {
      case "loading":
        return renderLoading()

      case "sign-in":
        return (
          <div className="activate-body">
            <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
              Sign in to decide
            </h1>
            <SignInForm next={nextPath} />
          </div>
        )

      case "error":
        return (
          <div className="activate-body">
            <div className="activate-icon activate-icon-warn" aria-hidden="true">
              <WarnIcon />
            </div>
            <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
              Something went wrong
            </h1>
            <p className="activate-copy" role="alert">
              {phase.message}
            </p>
            <div className="activate-actions">
              <button
                type="button"
                className="activate-btn activate-btn-approve"
                onClick={() => window.location.reload()}
              >
                Try again
              </button>
            </div>
          </div>
        )

      case "closed":
        return (
          <div className="activate-body">
            <div className="activate-icon activate-icon-neutral" aria-hidden="true">
              <ClockIcon />
            </div>
            <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
              {phase.title}
            </h1>
            <p className="activate-copy">{phase.message}</p>
            <p className="activate-footnote">
              You can close this tab. Nothing was shared with the agent.
            </p>
          </div>
        )

      case "done":
        return (
          <div className="activate-body">
            <div
              className={`activate-icon ${phase.decision === "approve" ? "activate-icon-ok" : "activate-icon-neutral"}`}
              aria-hidden="true"
            >
              {phase.decision === "approve" ? <CheckIcon /> : <DenyIcon />}
            </div>
            <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
              {phase.decision === "approve" ? "Approved" : "Denied"}
            </h1>
            {phase.decision === "approve" ? (
              <>
                <p className="activate-copy">
                  Return to your terminal. <strong className="activate-strong">{phase.clientName}</strong>{" "}
                  will collect its key on the next check.
                </p>
                <p className="activate-footnote">
                  The key is handed straight to the agent and never shown on this page. You can
                  revoke it later from your account.
                </p>
              </>
            ) : (
              <p className="activate-copy">
                Nothing was shared. <strong className="activate-strong">{phase.clientName}</strong>{" "}
                has been told you declined and cannot use your account.
              </p>
            )}
          </div>
        )

      case "review":
      case "deciding": {
        const { grant } = phase
        const busy = phase.kind === "deciding"
        return (
          <div className="activate-body">
            <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
              <span className="activate-agent">{grant.client_name}</span> wants an API key
            </h1>
            <p className="activate-copy">
              Approving gives it a key for{" "}
              <strong className="activate-strong">{grant.email}</strong>, letting it read model
              pricing as you.
            </p>

            <div className="activate-verify">
              <p className="activate-verify-label">
                Confirm this matches the code shown in your terminal
              </p>
              <code className="activate-code">{formatUserCode(normalizeUserCode(grant.user_code))}</code>
              <p className="activate-verify-meta">
                {grant.platform && (
                  <>
                    Requested from <span className="activate-mono">{grant.platform}</span>
                    <span className="activate-sep" aria-hidden="true">
                      ·
                    </span>
                  </>
                )}
                <span aria-live="polite">
                  {remainingMs > 0 ? (
                    <>
                      Expires in <span className="activate-timer">{formatRemaining(remainingMs)}</span>
                    </>
                  ) : (
                    "Expiring"
                  )}
                </span>
              </p>
            </div>

            <div className="activate-actions">
              <button
                type="button"
                className="activate-btn activate-btn-approve"
                onClick={() => void decide("approve")}
                disabled={busy}
                aria-busy={busy}
              >
                {busy ? <span className="signup-spinner" aria-hidden="true" /> : "Approve"}
              </button>
              <button
                type="button"
                className="activate-btn activate-btn-deny"
                onClick={() => void decide("deny")}
                disabled={busy}
              >
                Deny
              </button>
            </div>

            <p className="activate-footnote">
              Only approve if you started this in {grant.client_name}. If you did not, choose Deny
              and nothing will be shared.
            </p>
          </div>
        )
      }
    }
  }

  function renderLoading() {
    return (
      <div className="activate-body activate-centered" role="status" aria-label="Loading request">
        <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
        <p className="activate-loading-text">Loading the request…</p>
      </div>
    )
  }

  function renderCodeEntry() {
    return (
      <div className="activate-body">
        <h1 className="activate-heading" ref={headingRef} tabIndex={-1}>
          Enter your code
        </h1>
        <p className="activate-copy">
          The agent printed an eight-character code. Type it below, in the same two groups of four.
        </p>
        <form
          className="activate-body"
          onSubmit={(e) => {
            e.preventDefault()
            const normalized = normalizeUserCode(codeInput)
            if (!isValidUserCode(normalized)) {
              setCodeError("That code is not the right shape. It is eight characters, like ACDF-2345.")
              return
            }
            setCodeError(null)
            stashCode(normalized)
            setCode(normalized)
          }}
        >
          <div className="signup-field">
            <label htmlFor="activate-code" className="signup-label">
              Approval code
            </label>
            <input
              id="activate-code"
              type="text"
              required
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              placeholder="ACDF-2345"
              value={codeInput}
              onChange={(e) => {
                setCodeInput(e.target.value)
                if (codeError) setCodeError(null)
              }}
              className="signup-input activate-code-input"
              aria-invalid={codeError ? "true" : undefined}
              aria-describedby={codeError ? "activate-code-error" : undefined}
            />
          </div>
          {codeError && (
            <p id="activate-code-error" className="signup-error" role="alert">
              {codeError}
            </p>
          )}
          <button type="submit" className="activate-btn activate-btn-approve">
            Continue
          </button>
        </form>
      </div>
    )
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Remembers the code in this browser only, so the sign-in round trip can restore
 * it without the code ever travelling through an emailed URL. Failures (private
 * browsing, storage disabled) are non-fatal: the user can retype the code.
 */
function stashCode(normalized: string): void {
  try {
    sessionStorage.setItem(CODE_STORAGE_KEY, normalized)
  } catch {
    // Storage unavailable; the typed code still works for this page.
  }
}

function readStashedCode(): string | null {
  try {
    const stored = sessionStorage.getItem(CODE_STORAGE_KEY) ?? ""
    return isValidUserCode(stored) ? stored : null
  } catch {
    return null
  }
}

/** Renders a stored code as XXXX-XXXX, matching the terminal display. */
function formatUserCode(normalized: string): string {
  if (normalized.length !== 8) return normalized
  return `${normalized.slice(0, 4)}-${normalized.slice(4)}`
}

/** Formats remaining milliseconds as m:ss. */
function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${minutes}:${String(seconds).padStart(2, "0")}`
}

// ── Icons ─────────────────────────────────────────────────────────────────────

function CheckIcon() {
  return (
    <svg width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true">
      <circle cx="15" cy="15" r="13" stroke="currentColor" strokeWidth="1.75" />
      <path
        d="M9 15.5l4 4 8-9"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function DenyIcon() {
  return (
    <svg width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true">
      <circle cx="15" cy="15" r="13" stroke="currentColor" strokeWidth="1.75" />
      <path d="M10.5 10.5l9 9M19.5 10.5l-9 9" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  )
}

function ClockIcon() {
  return (
    <svg width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true">
      <circle cx="15" cy="15" r="13" stroke="currentColor" strokeWidth="1.75" />
      <path d="M15 8.5V15l5 3" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  )
}

function WarnIcon() {
  return (
    <svg width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true">
      <circle cx="15" cy="15" r="13" stroke="currentColor" strokeWidth="1.75" />
      <path d="M15 8.5v7M15 19.5v2" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  )
}
