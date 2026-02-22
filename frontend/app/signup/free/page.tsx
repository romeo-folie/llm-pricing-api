"use client"

import { useState } from "react"

type FormState =
  | "idle"
  | "loading"
  | "success"
  | "error-email"
  | "error-ratelimit"
  | "error-server"

const ERROR_MESSAGES: Partial<Record<FormState, string>> = {
  "error-email":     "Please enter a valid email address.",
  "error-ratelimit": "Too many requests. Try again tomorrow.",
  "error-server":    "Something went wrong. Please try again.",
}

function isValidEmail(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())
}

export default function FreeSignupPage() {
  const [email,     setEmail]     = useState("")
  const [formState, setFormState] = useState<FormState>("idle")

  const isLoading = formState === "loading"
  const isSuccess = formState === "success"
  const errorMsg  = ERROR_MESSAGES[formState] ?? null

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()

    if (!isValidEmail(email)) {
      setFormState("error-email")
      return
    }

    setFormState("loading")

    try {
      const res = await fetch("/api/signup/free", {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({ email: email.trim() }),
      })

      if (res.status === 429) { setFormState("error-ratelimit"); return }
      if (!res.ok)            { setFormState("error-server");    return }

      setFormState("success")
    } catch {
      setFormState("error-server")
    }
  }

  return (
    <main
      style={{
        minHeight:       "calc(100vh - 60px)",
        backgroundColor: "var(--bg)",
        display:         "flex",
        flexDirection:   "column",
        alignItems:      "center",
        justifyContent:  "center",
        padding:         "48px 24px",
      }}
    >
      <div style={{ width: "100%", maxWidth: "440px" }}>

        {/* Breadcrumb label */}
        <span
          className="font-orbitron text-xs tracking-widest"
          style={{ display: "block", color: "var(--dim)", marginBottom: "8px" }}
        >
          [ REQUEST ACCESS ]
        </span>

        {/* Rank badge */}
        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "24px" }}>
          <span className="font-orbitron text-xs" style={{ color: "var(--dim)" }}>▪</span>
          <span className="font-orbitron text-xs" style={{ color: "var(--dim)", letterSpacing: "0.1em" }}>
            RECRUIT
          </span>
        </div>

        {/* Heading */}
        <h1
          className="font-outfit text-3xl font-bold"
          style={{ color: "var(--ink)", marginBottom: "8px", lineHeight: 1.2 }}
        >
          Get your free API key
        </h1>
        <p
          className="font-outfit text-sm"
          style={{ color: "var(--muted)", marginBottom: "36px", lineHeight: 1.6 }}
        >
          100 requests/day. No credit card required.
          Your key arrives by email within seconds.
        </p>

        {/* Divider */}
        <div style={{ borderTop: "1px solid var(--borderDk)", marginBottom: "36px" }} />

        {isSuccess ? (
          /* ── Success state ── */
          <div
            style={{
              padding:         "20px 24px",
              backgroundColor: "var(--accentLt)",
              border:          "1px solid var(--accent)",
            }}
          >
            <div style={{ display: "flex", alignItems: "flex-start", gap: "12px" }}>
              <span style={{ color: "var(--accent)", flexShrink: 0, marginTop: "1px" }}>✓</span>
              <div>
                <p
                  className="font-orbitron text-xs"
                  style={{ letterSpacing: "0.1em", color: "var(--accent)", marginBottom: "6px" }}
                >
                  TRANSMISSION RECEIVED
                </p>
                <p
                  className="font-outfit text-sm"
                  style={{ color: "var(--ink)", margin: 0 }}
                >
                  Check your inbox — your key is on its way.
                </p>
              </div>
            </div>
          </div>
        ) : (
          /* ── Form ── */
          <form onSubmit={handleSubmit} noValidate>

            {/* Email field */}
            <div style={{ marginBottom: "16px" }}>
              <label
                htmlFor="signup-email"
                className="font-orbitron"
                style={{
                  display:       "block",
                  fontSize:      "0.6rem",
                  letterSpacing: "0.14em",
                  textTransform: "uppercase",
                  color:         "var(--muted)",
                  marginBottom:  "8px",
                }}
              >
                Email address
              </label>
              <input
                id="signup-email"
                type="email"
                autoComplete="email"
                autoCorrect="off"
                spellCheck={false}
                placeholder="you@example.com"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value)
                  if (formState.startsWith("error")) {
                    setFormState("idle")
                    // Input is already focused; onFocus won't re-fire, so apply accent border directly.
                    e.target.style.borderColor = "var(--accent)"
                  }
                }}
                disabled={isLoading}
                style={{
                  width:           "100%",
                  fontFamily:      "var(--font-outfit, sans-serif)",
                  fontSize:        "0.9rem",
                  padding:         "0.75rem 1rem",
                  backgroundColor: "var(--surfaceLo)",
                  border:          `1px solid ${formState === "error-email" ? "var(--red)" : "var(--border)"}`,
                  color:           "var(--ink)",
                  outline:         "none",
                  transition:      "border-color 0.15s",
                }}
                onFocus={(e) => {
                  if (formState !== "error-email") e.target.style.borderColor = "var(--accent)"
                }}
                onBlur={(e) => {
                  if (formState !== "error-email") e.target.style.borderColor = "var(--border)"
                }}
              />
            </div>

            {/* Error message */}
            {errorMsg && (
              <div
                className="font-outfit"
                style={{
                  fontSize:        "0.82rem",
                  color:           "var(--red)",
                  padding:         "0.5rem 0.75rem",
                  backgroundColor: "var(--redLt)",
                  border:          "1px solid color-mix(in srgb, var(--red) 25%, transparent)",
                  marginBottom:    "16px",
                }}
              >
                {errorMsg}
              </div>
            )}

            {/* Submit button */}
            <button
              type="submit"
              disabled={isLoading}
              style={{
                width:           "100%",
                fontFamily:      "var(--font-orbitron, monospace)",
                fontSize:        "0.7rem",
                letterSpacing:   "0.14em",
                textTransform:   "uppercase",
                padding:         "0.875rem 1.5rem",
                backgroundColor: isLoading ? "var(--muted)" : "var(--ink)",
                color:           "var(--bg)",
                border:          "none",
                cursor:          isLoading ? "not-allowed" : "pointer",
                display:         "flex",
                alignItems:      "center",
                justifyContent:  "center",
                gap:             "8px",
                transition:      "opacity 0.15s",
                opacity:         isLoading ? 0.7 : 1,
              }}
            >
              {isLoading ? (
                <>
                  <span
                    style={{
                      width:        "12px",
                      height:       "12px",
                      border:       "1.5px solid var(--bg)",
                      borderTopColor: "transparent",
                      borderRadius: "50%",
                      display:      "inline-block",
                      animation:    "free-signup-spin 0.7s linear infinite",
                    }}
                  />
                  Transmitting...
                </>
              ) : (
                "Send my key"
              )}
            </button>

          </form>
        )}
      </div>

      {/* Scoped keyframe for spinner */}
      <style>{`
        @keyframes free-signup-spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </main>
  )
}
