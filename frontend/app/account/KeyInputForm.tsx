"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";

export default function KeyInputForm() {
  const router = useRouter();
  const [key, setKey] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!key.trim()) return;

    setLoading(true);
    setError(null);

    try {
      const res = await fetch("/api/account/verify", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ key: key.trim() }),
      });

      if (res.ok) {
        router.refresh();
      } else {
        const data = await res.json().catch(() => ({}));
        setError(data.error ?? "Key verification failed. Check your key and try again.");
      }
    } catch {
      setError("Connection error. Please try again.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "1.5rem",
        width: "100%",
        maxWidth: "480px",
      }}
    >
      <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
        <label
          htmlFor="api-key"
          style={{
            fontFamily: "var(--font-orbitron, monospace)",
            fontSize: "0.65rem",
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            color: "var(--muted)",
          }}
        >
          API Key
        </label>
        <input
          id="api-key"
          type="password"
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          placeholder="sk-live-..."
          value={key}
          onChange={(e) => setKey(e.target.value)}
          disabled={loading}
          style={{
            fontFamily: "var(--font-orbitron, monospace)",
            fontSize: "0.85rem",
            letterSpacing: "0.04em",
            padding: "0.75rem 1rem",
            background: "var(--surfaceLo, #F5F0EB)",
            border: "1px solid var(--border)",
            borderRadius: "4px",
            color: "var(--ink)",
            outline: "none",
            width: "100%",
            boxSizing: "border-box",
            transition: "border-color 0.15s",
          }}
          onFocus={(e) => (e.target.style.borderColor = "var(--accent)")}
          onBlur={(e) => (e.target.style.borderColor = "var(--border)")}
        />
      </div>

      {error && (
        <p
          style={{
            fontSize: "0.8rem",
            color: "var(--red, #dc2626)",
            margin: 0,
            padding: "0.5rem 0.75rem",
            background: "color-mix(in srgb, var(--red, #dc2626) 8%, transparent)",
            border: "1px solid color-mix(in srgb, var(--red, #dc2626) 25%, transparent)",
            borderRadius: "4px",
          }}
        >
          {error}
        </p>
      )}

      <button
        type="submit"
        disabled={loading || !key.trim()}
        style={{
          fontFamily: "var(--font-orbitron, monospace)",
          fontSize: "0.7rem",
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          padding: "0.75rem 1.5rem",
          background: loading ? "var(--muted)" : "var(--ink)",
          color: "var(--bg)",
          border: "none",
          borderRadius: "4px",
          cursor: loading ? "not-allowed" : "pointer",
          alignSelf: "flex-start",
          transition: "opacity 0.15s",
          opacity: loading || !key.trim() ? 0.5 : 1,
        }}
      >
        {loading ? "Verifying…" : "Access Dashboard"}
      </button>
    </form>
  );
}
