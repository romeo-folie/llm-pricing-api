"use client";

import { useEffect, useState, useCallback } from "react";

interface UsageData {
  usageToday: number;
  dailyLimit: number;
}

interface Props {
  initialUsageToday: number;
  initialDailyLimit: number;
}

const POLL_INTERVAL_MS = 60_000; // 60 seconds

export default function UsageCard({ initialUsageToday, initialDailyLimit }: Props) {
  const [usage, setUsage] = useState<UsageData>({
    usageToday: initialUsageToday,
    dailyLimit: initialDailyLimit,
  });
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [polling, setPolling] = useState(false);
  const [pollError, setPollError] = useState<string | null>(null);

  const fetchUsage = useCallback(async () => {
    setPolling(true);
    try {
      const res = await fetch("/api/account/usage", { cache: "no-store" });
      if (res.ok) {
        const data: UsageData = await res.json();
        setUsage(data);
        setLastUpdated(new Date());
        setPollError(null);
      } else if (res.status === 401) {
        setPollError("Session expired — please sign in again.");
      } else {
        setPollError("Could not refresh usage data.");
      }
    } catch {
      setPollError("Connection error — retrying shortly.");
    } finally {
      setPolling(false);
    }
  }, []);

  useEffect(() => {
    const id = setInterval(fetchUsage, POLL_INTERVAL_MS);
    return () => clearInterval(id);
  }, [fetchUsage]);

  const { usageToday, dailyLimit } = usage;
  const unlimited = dailyLimit === -1;
  const pct = unlimited ? 0 : Math.min(100, Math.round((usageToday / dailyLimit) * 100));
  const barColor =
    pct >= 90 ? "var(--red, #dc2626)" : pct >= 70 ? "var(--yellow, #ca8a04)" : "var(--accent)";

  const fmt = (n: number) => n.toLocaleString();
  const updatedAt = lastUpdated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "1.25rem" }}>
      {/* counts */}
      <div style={{ display: "flex", alignItems: "baseline", gap: "0.5rem" }}>
        <span
          style={{
            fontFamily: "var(--font-orbitron, monospace)",
            fontSize: "2rem",
            fontWeight: 700,
            color: "var(--ink)",
            letterSpacing: "-0.02em",
          }}
        >
          {fmt(usageToday)}
        </span>
        <span style={{ fontSize: "0.85rem", color: "var(--muted)" }}>
          {unlimited ? "requests today" : `/ ${fmt(dailyLimit)}`}
        </span>
      </div>

      {/* progress bar */}
      {!unlimited && (
        <div>
          <div
            style={{
              height: "4px",
              background: "var(--border)",
              borderRadius: "2px",
              overflow: "hidden",
            }}
          >
            <div
              style={{
                height: "100%",
                width: `${pct}%`,
                background: barColor,
                borderRadius: "2px",
                transition: "width 0.6s cubic-bezier(0.4, 0, 0.2, 1)",
              }}
            />
          </div>
          <p
            style={{
              margin: "0.4rem 0 0",
              fontSize: "0.72rem",
              color: "var(--muted)",
              letterSpacing: "0.03em",
            }}
          >
            {pct}% of daily quota
          </p>
        </div>
      )}

      {unlimited && (
        <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--accent)" }}>
          Unlimited requests
        </p>
      )}

      {/* poll error */}
      {pollError && (
        <p
          style={{
            margin: 0,
            fontSize: "0.72rem",
            color: "var(--red, #dc2626)",
            padding: "0.35rem 0.6rem",
            background: "color-mix(in srgb, var(--red, #dc2626) 8%, transparent)",
            border: "1px solid color-mix(in srgb, var(--red, #dc2626) 25%, transparent)",
            borderRadius: "4px",
          }}
        >
          {pollError}
        </p>
      )}

      {/* last updated */}
      <p
        style={{
          margin: 0,
          fontSize: "0.68rem",
          color: "var(--dim, #a8a29e)",
          letterSpacing: "0.03em",
        }}
      >
        {polling ? "Updating…" : `Updated at ${updatedAt} · auto-refreshes every 60s`}
      </p>
    </div>
  );
}
