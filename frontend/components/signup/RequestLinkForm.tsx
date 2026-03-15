"use client";

import React, { useRef, useState, useTransition } from "react";
import { requestMagicLink, type ApiError } from "@/lib/signup";

interface Props {
  onSent: (email: string) => void;
}

export default function RequestLinkForm({ onSent }: Props) {
  const [email, setEmail] = useState("");
  const [error, setError] = useState<ApiError | null>(null);
  const [cooldownMs, setCooldownMs] = useState(0);
  const [isPending, startTransition] = useTransition();
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

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

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (isPending || cooldownMs > 0) return;
    setError(null);

    startTransition(async () => {
      const result = await requestMagicLink(email.trim());
      if (result.ok) {
        onSent(email.trim());
      } else {
        setError(result.error);
        if (result.error.retryAfterMs) {
          startCooldownTimer(result.error.retryAfterMs);
        }
      }
    });
  };

  const cooldownSec = Math.ceil(cooldownMs / 1000);
  const isBlocked = isPending || cooldownMs > 0;

  return (
    <form onSubmit={handleSubmit} className="signup-form" noValidate>
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
            setEmail(e.target.value);
            if (error) setError(null);
          }}
          disabled={isBlocked}
          className="signup-input"
          aria-describedby={error ? "signup-error" : undefined}
          aria-invalid={error ? "true" : undefined}
        />
      </div>

      {error && (
        <p id="signup-error" className="signup-error" role="alert">
          {error.message}
        </p>
      )}

      <button
        type="submit"
        disabled={isBlocked || !email.trim()}
        className="signup-submit"
        aria-busy={isPending}
      >
        {isPending ? (
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
  );
}
