"use client";

import React, { useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { getIdentity, type IdentityResponse, type SignupStep } from "@/lib/signup";
import RequestLinkForm from "@/components/signup/RequestLinkForm";
import SentConfirmation from "@/components/signup/SentConfirmation";
import KeyPanel from "@/components/signup/KeyPanel";

/**
 * SignupFlow orchestrates the state machine:
 *
 *  request  →  sent  (after form submission)
 *  ↓ (on page load with ?verified=1 and valid session)
 *  verified  →  issued / already-issued
 *
 * The verify callback (GET /auth/signup/verify?token=...) is handled entirely
 * server-side. The backend marks the identity verified, creates a session
 * cookie, and redirects to /signup/free?verified=1. This component detects
 * that param and immediately calls /auth/signup/me to retrieve identity state.
 */
export default function SignupFlow() {
  const searchParams = useSearchParams();
  const verified = searchParams.get("verified") === "1";

  const [step, setStep] = useState<SignupStep>(verified ? "verifying" : "request");
  const [sentEmail, setSentEmail] = useState("");
  const [identity, setIdentity] = useState<IdentityResponse | null>(null);
  const [verifyError, setVerifyError] = useState<string | null>(null);

  const fetchedRef = useRef(false);

  useEffect(() => {
    if (!verified || fetchedRef.current) return;
    fetchedRef.current = true;

    const controller = new AbortController();
    (async () => {
      try {
        const result = await getIdentity(controller.signal);
        if (result.ok) {
          setIdentity(result.identity);
          setStep(result.identity.has_active_key ? "already-issued" : "verified");
        } else {
          setVerifyError(
            "We couldn't confirm your session. The link may have expired — try again."
          );
          setStep("error");
        }
      } catch (err) {
        if ((err as Error).name === "AbortError") return;
        throw err;
      }
    })();

    return () => controller.abort();
  }, [verified]);

  // ── Render ────────────────────────────────────────────────────────────────

  if (step === "verifying") {
    return (
      <div className="signup-flow-center" role="status" aria-label="Verifying">
        <span className="signup-spinner signup-spinner-lg" aria-hidden="true" />
        <p className="signup-flow-status">Verifying your link…</p>
      </div>
    );
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
            setStep("request");
            setVerifyError(null);
          }}
        >
          Try again
        </button>
      </div>
    );
  }

  if ((step === "verified" || step === "already-issued") && identity) {
    return <KeyPanel identity={identity} />;
  }

  if (step === "sent") {
    return (
      <SentConfirmation
        email={sentEmail}
        onBack={() => {
          setSentEmail("");
          setStep("request");
        }}
      />
    );
  }

  // Default: request step
  return (
    <RequestLinkForm
      onSent={(email) => {
        setSentEmail(email);
        setStep("sent");
      }}
    />
  );
}
