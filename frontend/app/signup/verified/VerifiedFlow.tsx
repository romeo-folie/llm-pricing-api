"use client"

import { useEffect, useRef, useState } from "react"
import { getIdentity, type IdentityResponse, type ApiError } from "@/lib/signup"
import VerifiedKeyPanel from "./VerifiedKeyPanel"
import "./verified.css"

type FlowState =
  | { phase: "loading" }
  | { phase: "ready"; identity: IdentityResponse }
  | { phase: "error"; message: string }

export default function VerifiedFlow() {
  const [state, setState] = useState<FlowState>({ phase: "loading" })
  const fetchedRef = useRef(false)

  useEffect(() => {
    if (fetchedRef.current) return
    fetchedRef.current = true

    const controller = new AbortController()
    ;(async () => {
      try {
        const result = await getIdentity(controller.signal)
        if (controller.signal.aborted) return
        if (result.ok) {
          setState({ phase: "ready", identity: result.identity })
        } else {
          setState({
            phase: "error",
            message: "We couldn't confirm your session. The link may have expired.",
          })
        }
      } catch (err) {
        if ((err as Error).name === "AbortError") return
        setState({ phase: "error", message: "Something went wrong. Please try again." })
      }
    })()

    return () => controller.abort()
  }, [])

  if (state.phase === "loading") {
    return (
      <div className="verified-loading animate-reveal-card" role="status" aria-label="Loading your key">
        <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
        <p className="verified-loading-text">Preparing your API key…</p>
      </div>
    )
  }

  if (state.phase === "error") {
    return (
      <div className="verified-card animate-reveal-card">
        <div className="verified-icon verified-icon-error" aria-hidden="true">
          <ErrorIcon />
        </div>
        <h1 className="verified-heading">Link expired</h1>
        <p className="verified-sub">{state.message}</p>
        <a href="/signup/free" className="verified-btn">
          Request a new link
        </a>
      </div>
    )
  }

  return <VerifiedKeyPanel identity={state.identity} />
}

function ErrorIcon() {
  return (
    <svg width="32" height="32" viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <circle cx="16" cy="16" r="14" stroke="currentColor" strokeWidth="2" />
      <path d="M16 9v8M16 21v2" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
    </svg>
  )
}
