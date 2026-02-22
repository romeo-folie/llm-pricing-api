"use client";

import { useState, type CSSProperties } from "react";
import UsageCard from "@/components/UsageCard";
import type { AccountSession } from "@/lib/session";

interface Props {
  session: AccountSession;
  checkoutDev?: string;
  checkoutPro?: string;
}

const TIER_LABELS: Record<string, string> = {
  free: "FREE",
  developer: "DEVELOPER",
  pro: "PRO",
};

const TIER_COLORS: Record<string, string> = {
  free: "var(--muted)",
  developer: "var(--accent)",
  pro: "var(--purple, #7C3AED)",
};


export default function AccountDashboard({ session, checkoutDev, checkoutPro }: Props) {
  const [copied, setCopied] = useState(false);

  const tier = session.tier ?? "free";
  const tierLabel = TIER_LABELS[tier] ?? tier.toUpperCase();
  const tierColor = TIER_COLORS[tier] ?? "var(--muted)";
  const isFree = tier === "free";
  const isPro = tier === "pro";
  const CHECKOUT_DEV = checkoutDev;
  const CHECKOUT_PRO = checkoutPro;

  function copyKey() {
    navigator.clipboard.writeText(session.maskedKey).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    });
  }

  const cardStyle = (delay: string): CSSProperties => ({
    background: "var(--surface)",
    border: "1px solid var(--border)",
    borderRadius: "6px",
    padding: "1.5rem 1.75rem",
    display: "flex",
    flexDirection: "column",
    gap: "1rem",
    animation: "account-rise 0.5s ease both",
    animationDelay: delay,
  });

  const sectionLabel: CSSProperties = {
    fontFamily: "var(--font-orbitron, monospace)",
    fontSize: "0.6rem",
    letterSpacing: "0.15em",
    textTransform: "uppercase",
    color: "var(--muted)",
    margin: 0,
  };

  return (
    <>
      <style>{`
        @keyframes account-rise {
          from { opacity: 0; transform: translateY(10px); }
          to   { opacity: 1; transform: translateY(0); }
        }
      `}</style>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))",
          gap: "1rem",
          width: "100%",
        }}
      >
        {/* ── Plan card ── */}
        <div style={cardStyle("0ms")}>
          <p style={sectionLabel}>Plan</p>

          <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
            <span
              style={{
                fontFamily: "var(--font-orbitron, monospace)",
                fontSize: "0.65rem",
                letterSpacing: "0.12em",
                padding: "0.25rem 0.6rem",
                border: `1px solid ${tierColor}`,
                borderRadius: "3px",
                color: tierColor,
              }}
            >
              [ {tierLabel} ]
            </span>
            {session.endsAt && (
              <span style={{ fontSize: "0.75rem", color: "var(--red, #dc2626)" }}>
                Cancels {new Date(session.endsAt).toLocaleDateString()}
              </span>
            )}
          </div>

          {isFree && (CHECKOUT_DEV || CHECKOUT_PRO) && (
            <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem", marginTop: "0.25rem" }}>
              <p style={{ ...sectionLabel, fontSize: "0.65rem" }}>Upgrade</p>
              <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
                {CHECKOUT_DEV && (
                  <a
                    href={CHECKOUT_DEV}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      fontFamily: "var(--font-orbitron, monospace)",
                      fontSize: "0.65rem",
                      letterSpacing: "0.1em",
                      padding: "0.4rem 0.9rem",
                      background: "var(--ink)",
                      color: "var(--bg)",
                      borderRadius: "4px",
                      textDecoration: "none",
                    }}
                  >
                    Developer — $15/mo
                  </a>
                )}
                {CHECKOUT_PRO && (
                  <a
                    href={CHECKOUT_PRO}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      fontFamily: "var(--font-orbitron, monospace)",
                      fontSize: "0.65rem",
                      letterSpacing: "0.1em",
                      padding: "0.4rem 0.9rem",
                      background: "var(--purple, #7C3AED)",
                      color: "#fff",
                      borderRadius: "4px",
                      textDecoration: "none",
                    }}
                  >
                    Pro — $50/mo
                  </a>
                )}
              </div>
            </div>
          )}

          {!isFree && session.portalUrl && (
            <a
              href={session.portalUrl}
              target="_blank"
              rel="noopener noreferrer"
              style={{
                fontFamily: "var(--font-orbitron, monospace)",
                fontSize: "0.65rem",
                letterSpacing: "0.1em",
                color: "var(--accent)",
                textDecoration: "none",
                alignSelf: "flex-start",
                borderBottom: "1px solid var(--accent)",
                paddingBottom: "1px",
              }}
            >
              Manage subscription →
            </a>
          )}
        </div>

        {/* ── API Key card ── */}
        <div style={cardStyle("80ms")}>
          <p style={sectionLabel}>API Key</p>

          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: "0.75rem",
              flexWrap: "wrap",
            }}
          >
            <code
              style={{
                fontFamily: "var(--font-orbitron, monospace)",
                fontSize: "0.8rem",
                letterSpacing: "0.04em",
                color: "var(--ink)",
                background: "var(--surfaceLo, #EDE8E2)",
                padding: "0.4rem 0.75rem",
                borderRadius: "4px",
                border: "1px solid var(--border)",
                wordBreak: "break-all",
              }}
            >
              {session.maskedKey}
            </code>

            <button
              onClick={copyKey}
              aria-label="Copy masked key"
              style={{
                fontFamily: "var(--font-orbitron, monospace)",
                fontSize: "0.6rem",
                letterSpacing: "0.1em",
                textTransform: "uppercase",
                padding: "0.35rem 0.7rem",
                background: copied ? "var(--accentLt)" : "transparent",
                border: `1px solid ${copied ? "var(--accent)" : "var(--border)"}`,
                borderRadius: "4px",
                color: copied ? "var(--accent)" : "var(--muted)",
                cursor: "pointer",
                transition: "all 0.15s",
                whiteSpace: "nowrap",
              }}
            >
              {copied ? "Copied ✓" : "Copy"}
            </button>
          </div>

          <p
            style={{
              margin: 0,
              fontSize: "0.72rem",
              color: "var(--dim)",
            }}
          >
            Shown partially for security. Use your original key for API calls.
          </p>
        </div>

        {/* ── Usage card ── */}
        <div style={{ ...cardStyle("160ms"), gridColumn: isPro ? undefined : undefined }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <p style={sectionLabel}>Today&apos;s Usage</p>
            <span
              style={{
                fontSize: "0.6rem",
                letterSpacing: "0.08em",
                color: "var(--dim)",
                fontFamily: "var(--font-orbitron, monospace)",
              }}
            >
              UTC
            </span>
          </div>

          <UsageCard
            initialUsageToday={session.usageToday}
            initialDailyLimit={session.dailyLimit}
          />
        </div>
      </div>
    </>
  );
}
