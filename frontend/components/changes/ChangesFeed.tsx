"use client"

import { useEffect, useRef, useState, useCallback } from "react"
import { useRouter, usePathname } from "next/navigation"
import type { PriceChange, Provider, ChangesSummary, ChangesPageResult } from "@/lib/api"
import type { TimeWindow } from "@/lib/changes-aggregation"
import ChangeRow from "./ChangeRow"
import TimeWindowSelector from "./TimeWindowSelector"
import PriceHeatmap from "./PriceHeatmap"
import BiggestMovers from "./BiggestMovers"
import EmptyState from "@/components/ui/EmptyState"

interface ChangesFeedProps {
  initialChanges: PriceChange[]
  initialTotal: number
  initialHasMore: boolean
  initialNextCursor: string | null
  initialSummary: ChangesSummary | null
  providers: Provider[]
  initialProvider?: string
  initialSince?: string
}

const POLL_INTERVAL = 60_000

export default function ChangesFeed({
  initialChanges,
  initialTotal,
  initialHasMore,
  initialNextCursor,
  initialSummary,
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
  const [timeWindow,  setTimeWindow]  = useState<TimeWindow>("7d")

  // Pagination state
  const [total,       setTotal]       = useState(initialTotal)
  const [hasMore,     setHasMore]     = useState(initialHasMore)
  const [nextCursor,  setNextCursor]  = useState<string | null>(initialNextCursor)
  const [loadingMore, setLoadingMore] = useState(false)

  // Summary state (for heatmap + leaderboard)
  const [summary, setSummary] = useState<ChangesSummary | null>(initialSummary)

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const knownIdsRef = useRef<Set<string>>(new Set(initialChanges.map((c) => c.id)))
  // Generation counter — incremented on every filter change to discard stale
  // in-flight responses from loadMore or applyFilter.
  const generationRef = useRef(0)

  function buildUrl(prov: string, snc: string) {
    const params = new URLSearchParams()
    if (prov) params.set("provider", prov)
    if (snc)  params.set("since", snc)
    return params.toString() ? `${pathname}?${params.toString()}` : pathname
  }

  async function fetchChangesPage(
    prov: string,
    snc: string,
    before?: string,
    limit?: number,
  ): Promise<ChangesPageResult> {
    const params = new URLSearchParams()
    if (prov) params.set("provider", prov)
    if (snc)  params.set("since", snc)
    if (before) params.set("before", before)
    if (limit) params.set("limit", String(limit))
    const qs = params.toString() ? `?${params.toString()}` : ""
    const res = await fetch(`/api/changes${qs}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<ChangesPageResult>
  }

  const fetchSummary = useCallback(async (
    win: string,
    prov: string,
  ) => {
    const params = new URLSearchParams()
    params.set("window", win)
    if (prov) params.set("provider", prov)
    const qs = `?${params.toString()}`
    const res = await fetch(`/api/changes/summary${qs}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<ChangesSummary>
  }, [])

  function startPolling(prov: string, snc: string) {
    if (intervalRef.current) clearInterval(intervalRef.current)
    intervalRef.current = setInterval(async () => {
      try {
        const result = await fetchChangesPage(prov, snc)
        const incoming = result.data.filter((c) => !knownIdsRef.current.has(c.id))
        if (incoming.length > 0) {
          const freshIds = new Set(incoming.map((c) => c.id))
          setNewIds(freshIds)
          setChanges((prev) => [...incoming, ...prev])
          setTotal(result.total)
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
    generationRef.current++
    const gen = generationRef.current

    setProvider(prov)
    setSince(snc)
    knownIdsRef.current = new Set()
    setNextCursor(null)
    setHasMore(false)
    setLoadingMore(false)
    router.push(buildUrl(prov, snc), { scroll: false })

    // Refetch first page + summary
    fetchChangesPage(prov, snc)
      .then((result) => {
        if (generationRef.current !== gen) return // stale, discard
        setChanges(result.data)
        setTotal(result.total)
        setHasMore(result.hasMore)
        setNextCursor(result.nextCursor)
        knownIdsRef.current = new Set(result.data.map((c) => c.id))
        setPollFailed(false)
      })
      .catch(() => {
        if (generationRef.current !== gen) return
        setPollFailed(true)
      })

    fetchSummary(timeWindow, prov)
      .then((s) => {
        if (generationRef.current !== gen) return
        setSummary(s)
      })
      .catch(() => {/* summary failure is non-critical */})

    startPolling(prov, snc)
  }

  function handleTimeWindowChange(win: TimeWindow) {
    setTimeWindow(win)
    // Fetch new summary for the selected window
    fetchSummary(win, provider)
      .then(setSummary)
      .catch(() => {/* summary failure is non-critical */})
  }

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    const gen = generationRef.current
    setLoadingMore(true)
    try {
      const result = await fetchChangesPage(provider, since, nextCursor)
      if (generationRef.current !== gen) return // filter changed while loading
      setChanges((prev) => [...prev, ...result.data])
      setTotal(result.total)
      setHasMore(result.hasMore)
      setNextCursor(result.nextCursor)
      result.data.forEach((c) => knownIdsRef.current.add(c.id))
    } catch {
      // Keep existing state on failure
    } finally {
      if (generationRef.current === gen) setLoadingMore(false)
    }
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
        <div>
          <span
            className="font-orbitron text-xs tracking-widest"
            style={{ color: "var(--dim)", display: "block", marginBottom: "8px" }}
          >
            [ PRICE CHANGES ]
          </span>
          <h1
            className="font-outfit text-2xl font-bold"
            style={{ color: "var(--ink)" }}
          >
            Price Changes
          </h1>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
          {total > 0 && (
            <span
              className="font-outfit text-xs"
              style={{ color: "var(--muted)" }}
            >
              {total} tracked
            </span>
          )}
          <span
            className="font-orbitron text-xs"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "5px",
              padding: "1px 6px",
              border: "none",
              color: "#ffffff",
              backgroundColor: polling && !pollFailed ? "#10B981" : "var(--muted)",
              letterSpacing: "0.12em",
              fontSize: "0.65rem",
              lineHeight: 1.4,
            }}
          >
            <span
              className={polling && !pollFailed ? "animate-live" : ""}
              style={{
                width: "5px",
                height: "5px",
                borderRadius: "50%",
                backgroundColor: "rgba(255,255,255,0.7)",
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

      {/* Time window selector */}
      <TimeWindowSelector value={timeWindow} onChange={handleTimeWindowChange} />

      {/* Dashboard: Heatmap + Leaderboard */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 280px",
          gap: "16px",
          marginBottom: "24px",
        }}
        className="dashboard-grid"
      >
        <PriceHeatmap
          heatmap={summary?.heatmap ?? null}
          onCellClick={(prov) => applyFilter(prov, since)}
        />
        <BiggestMovers topMovers={summary?.top_movers ?? null} />
      </div>

      {/* Feed section label */}
      <span
        className="font-orbitron text-xs tracking-widest"
        style={{
          color: "var(--dim)",
          display: "block",
          marginBottom: "12px",
        }}
      >
        [ LIVE FEED ]
      </span>

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
        }}
      >
        <select
          value={provider}
          onChange={(e) => applyFilter(e.target.value, since)}
          className="font-outfit text-sm"
          style={{
            padding: "6px 32px 6px 12px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--bg)",
            color: "var(--ink)",
            cursor: "pointer",
            outline: "none",
            appearance: "none",
            backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2378716C' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
            backgroundRepeat: "no-repeat",
            backgroundPosition: "right 10px center",
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
            padding: "6px 12px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--bg)",
            color: "var(--ink)",
            outline: "none",
          }}
        />

        {(provider || since) && (
          <button
            onClick={() => applyFilter("", "")}
            className="font-outfit text-sm"
            style={{
              padding: "6px 12px",
              border: "1px solid var(--border)",
              color: "var(--muted)",
              backgroundColor: "var(--bg)",
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
        <EmptyState padding="80px 24px">No price changes yet.</EmptyState>
      ) : (
        <>
          {changes.map((change) => (
            <ChangeRow
              key={change.id}
              change={change}
              isNew={newIds.has(change.id)}
            />
          ))}

          {/* Load more */}
          {hasMore && (
            <div
              style={{
                display: "flex",
                justifyContent: "center",
                padding: "20px 0",
              }}
            >
              <button
                onClick={loadMore}
                disabled={loadingMore}
                className="font-outfit text-sm"
                style={{
                  padding: "8px 24px",
                  border: "1px solid var(--border)",
                  backgroundColor: "var(--surface)",
                  color: loadingMore ? "var(--dim)" : "var(--accent)",
                  cursor: loadingMore ? "default" : "pointer",
                  transition: "background-color 0.15s, color 0.15s",
                }}
              >
                {loadingMore ? "Loading..." : "Load more"}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}
