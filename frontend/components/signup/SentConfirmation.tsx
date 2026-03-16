"use client";

import React, { useEffect, useRef, useState, useTransition } from "react";
import { requestMagicLink, type ApiError } from "@/lib/signup";

interface Props {
  email: string;
  onBack: () => void;
}

export default function SentConfirmation({ email, onBack }: Props) {
  const [resent, setResent] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [isPending, startTransition] = useTransition();
  const [cooldownMs, setCooldownMs] = useState(0);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, []);

  const startCooldownTimer = (ms: number) => {
    setCooldownMs(ms);
    if (timerRef.current) clearInterval(timerRef.current);
    timerRef.current = setInterval(() => {
      setCooldownMs((prev) => {
        if (prev <= 1000) {
          clearInterval(timerRef.current!);
          return 0;
        }
        return prev - 1000;
      });
    }, 1000);
  };

  const handleResend = () => {
    if (isPending || cooldownMs > 0) return;
    setError(null);
    setResent(false);

    startTransition(async () => {
      const result = await requestMagicLink(email);
      if (result.ok) {
        setResent(true);
        startCooldownTimer(60_000);
      } else {
        setError(result.error);
        if (result.error.retryAfterMs) {
          startCooldownTimer(result.error.retryAfterMs);
        }
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
          disabled={isPending || cooldownMs > 0}
          className="signup-link-btn"
          aria-busy={isPending}
        >
          {isPending ? "Sending…" : cooldownMs > 0 ? `Resend in ${Math.ceil(cooldownMs / 1000)}s` : "Resend link"}
        </button>
        <span className="signup-sent-sep" aria-hidden="true">·</span>
        <button onClick={onBack} className="signup-link-btn">
          Use different email
        </button>
      </div>
    </div>
  );
}
