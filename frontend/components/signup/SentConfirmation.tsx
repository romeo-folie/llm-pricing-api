"use client";

import React, { useState, useTransition } from "react";
import { requestMagicLink, type ApiError } from "@/lib/signup";

interface Props {
  email: string;
  onBack: () => void;
}

export default function SentConfirmation({ email, onBack }: Props) {
  const [resent, setResent] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [isPending, startTransition] = useTransition();

  const handleResend = () => {
    if (isPending) return;
    setError(null);
    setResent(false);

    startTransition(async () => {
      const result = await requestMagicLink(email);
      if (result.ok) {
        setResent(true);
      } else {
        setError(result.error);
      }
    });
  };

  return (
    <div className="signup-sent">
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
        It expires in 15 minutes. No code to copy — just click the link.
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
          disabled={isPending}
          className="signup-link-btn"
          aria-busy={isPending}
        >
          {isPending ? "Sending…" : "Resend link"}
        </button>
        <span className="signup-sent-sep" aria-hidden="true">·</span>
        <button onClick={onBack} className="signup-link-btn">
          Use different email
        </button>
      </div>
    </div>
  );
}
