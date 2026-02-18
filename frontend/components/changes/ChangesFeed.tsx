"use client"

import { useEffect, useRef, useState } from "react"
import { useRouter, usePathname } from "next/navigation"
import type { PriceChange, Provider } from "@/lib/api"
import ChangeRow from "./ChangeRow"

interface ChangesFeedProps {
  initialChanges: PriceChange[]
  providers: Provider[]
  initialProvider?: string
  initialSince?: string
}

const POLL_INTERVAL = 60_000

export default function ChangesFeed({
  initialChanges,
  providers,
  initialProvider = "",
  initialSince    = "",
}: ChangesFeedProps) {
  const router   = useRouter()
  const pathname = usePathname()

  const [changes,     setChanges]     = useState<PriceChange[]>(initialChanges)
  const [newIds,      setNewIds]      = useState<Set<string>>(new Set())
  const [provider,    setProvider]    = useState(initialProvider)
  const [since,       setSince]       = useState(initialSince)
  const [polling,     setPolling]     = useState(true)
  const [pollFailed,  setPollFailed]  = useState(false)

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const knownIdsRef = useRef<Set<string>>(new Set(initialChanges.map((c) => c.id)))

  function buildUrl(prov: string, snc: string) {
    const params = new URLSearchParams()
    if (prov) params.set("provider", prov)
    if (snc)  params.set("since", snc)
    return params.toString() ? `${pathname}?${params.toString()}` : pathname
  }

  async function fetchChanges(prov: string, snc: string) {
    const params = new URLSearchParams()
    if (prov) params.set("provider", prov)
    if (snc)  params.set("since", snc)
    const qs = params.toString() ? `?${params.toString()}` : ""
    const res = await fetch(`/api/changes${qs}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<PriceChange[]>
  }

  function startPolling(prov: string, snc: string) {
    if (intervalRef.current) clearInterval(intervalRef.current)
    intervalRef.current = setInterval(async () => {
      try {
        const fresh   = await fetchChanges(prov, snc)
        const incoming = fresh.filter((c) => !knownIdsRef.current.has(c.id))
        if (incoming.length > 0) {
          const freshIds = new Set(incoming.map((c) => c.id))
          setNewIds(freshIds)
          setChanges((prev) => [...incoming, ...prev])
          incoming.forEach((c) => knownIdsRef.current.add(c.id))
          // Clear flash after animation duration
          setTimeout(() => setNewIds(new Set()), 600)
        }
        setPollFailed(false)
      } catch {
        setPollFailed(true)
      }
    }, POLL_INTERVAL)
  }

  useEffect(() => {
    startPolling(provider, since)
    setPolling(true)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function applyFilter(prov: string, snc: string) {
    setProvider(prov)
    setSince(snc)
    knownIdsRef.current = new Set()
    router.push(buildUrl(prov, snc), { scroll: false })

    // Immediate refetch
    fetchChanges(prov, snc)
      .then((data) => {
        setChanges(data)
        knownIdsRef.current = new Set(data.map((c) => c.id))
        setPollFailed(false)
      })
      .catch(() => setPollFailed(true))

    startPolling(prov, snc)
  }

  const liveColor = pollFailed ? "var(--dim)" : "var(--accent)"

  return (
    <div>
      {/* Section header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: "12px",
          marginBottom: "16px",
        }}
      >
        <h1
          className="font-orbitron text-2xl font-bold"
          style={{ color: "var(--ink)" }}
        >
          Price Changes
        </h1>

        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
          <span
            className="font-orbitron text-xs"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "5px",
              padding: "3px 8px",
              border: `1px solid ${liveColor}`,
              borderRadius: "2px",
              color: liveColor,
              backgroundColor: polling && !pollFailed ? "var(--accentLt)" : "var(--surfaceLo)",
            }}
          >
            <span
              className={polling && !pollFailed ? "animate-live" : ""}
              style={{
                width: "6px",
                height: "6px",
                borderRadius: "50%",
                backgroundColor: liveColor,
                display: "inline-block",
              }}
            />
            {pollFailed ? "PAUSED" : "LIVE"}
          </span>
          <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
            polling every 60s
          </span>
        </div>
      </div>

      {/* Filters */}
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: "8px",
          marginBottom: "16px",
          padding: "12px",
          border: "1px solid var(--border)",
          backgroundColor: "var(--surfaceLo)",
          borderRadius: "2px",
        }}
      >
        <select
          value={provider}
          onChange={(e) => applyFilter(e.target.value, since)}
          className="font-outfit text-sm"
          style={{
            padding: "6px 10px",
            border: "1px solid var(--border)",
            borderRadius: "2px",
            backgroundColor: "var(--surface)",
            color: "var(--text)",
          }}
        >
          <option value="">All providers</option>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>{p.name}</option>
          ))}
        </select>

        <input
          type="date"
          value={since}
          onChange={(e) => applyFilter(provider, e.target.value)}
          className="font-outfit text-sm"
          style={{
            padding: "6px 10px",
            border: "1px solid var(--border)",
            borderRadius: "2px",
            backgroundColor: "var(--surface)",
            color: "var(--text)",
          }}
        />

        {(provider || since) && (
          <button
            onClick={() => applyFilter("", "")}
            className="font-outfit text-sm"
            style={{
              padding: "6px 12px",
              border: "1px solid var(--border)",
              borderRadius: "2px",
              color: "var(--muted)",
              backgroundColor: "var(--surface)",
              cursor: "pointer",
            }}
          >
            Clear
          </button>
        )}
      </div>

      {/* Column header */}
      <div
        className="font-orbitron text-xs"
        style={{
          display: "flex",
          padding: "8px 16px",
          color: "var(--dim)",
          borderBottom: "2px solid var(--borderDk)",
          gap: "12px",
          textTransform: "uppercase",
          letterSpacing: "0.08em",
        }}
      >
        <span style={{ flex: 1 }}>Model</span>
        <span style={{ minWidth: "180px" }}>Price Change</span>
        <span style={{ minWidth: "64px", textAlign: "right" }}>Delta</span>
        <span style={{ minWidth: "72px", textAlign: "right" }}>Source</span>
      </div>

      {/* Rows */}
      {changes.length === 0 ? (
        <div
          className="font-outfit text-sm"
          style={{
            position: "relative",
            padding: "80px 24px",
            textAlign: "center",
            color: "var(--muted)",
            borderBottom: "1px solid var(--border)",
            overflow: "hidden",
          }}
        >
          {/* Isometric grid overlay */}
          <svg
            aria-hidden="true"
            style={{
              position: "absolute",
              inset: 0,
              width: "100%",
              height: "100%",
              opacity: 0.06,
              pointerEvents: "none",
            }}
          >
            <defs>
              <pattern
                id="empty-grid"
                x="0" y="0"
                width="40" height="40"
                patternUnits="userSpaceOnUse"
                patternTransform="rotate(-30) skewX(20)"
              >
                <path d="M 40 0 L 0 0 0 40" fill="none" stroke="var(--borderDk)" strokeWidth="0.5" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#empty-grid)" />
          </svg>
          <span style={{ position: "relative" }}>No price changes yet.</span>
        </div>
      ) : (
        changes.map((change) => (
          <ChangeRow
            key={change.id}
            change={change}
            isNew={newIds.has(change.id)}
          />
        ))
      )}
    </div>
  )
}
