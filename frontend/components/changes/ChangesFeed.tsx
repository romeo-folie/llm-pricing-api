"use client"

import { useEffect, useRef, useState, useCallback } from "react"
import { useRouter, usePathname } from "next/navigation"
import type { PriceChange, Provider, ChangesSummary, ChangesPageResult } from "@/lib/api"
import type { TimeWindow } from "@/lib/changes-aggregation"
import ChangeRow from "./ChangeRow"
import TimeWindowSelector from "./TimeWindowSelector"
import PriceHeatmap from "./PriceHeatmap"
import BiggestMovers from "./BiggestMovers"
import { DatePicker } from "@/components/ui/date-picker"
import EmptyState from "@/components/ui/EmptyState"
import { SiteBadge } from "@/components/ui/SiteBadge"

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

/** Convert a TimeWindow label to an ISO 8601 timestamp for the feed `since` param. */
function windowToSince(win: TimeWindow): string {
  const ms: Record<TimeWindow, number> = {
    "24h": 24 * 60 * 60_000,
    "7d":  7 * 24 * 60 * 60_000,
    "30d": 30 * 24 * 60 * 60_000,
  }
  return new Date(Date.now() - ms[win]).toISOString()
}

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
  // Whether the feed since is driven by the time window selector (true)
  // or a custom date from the DatePicker (false).
  const [syncedToWindow, setSyncedToWindow] = useState(!initialSince)

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
    // On mount, if no explicit since was provided via URL, sync the feed to
    // the default time window (7d) so the feed matches the summary/movers.
    if (!initialSince) {
      const winSince = windowToSince(timeWindow)
      setSince(winSince)

      fetchChangesPage(provider, winSince)
        .then((result) => {
          setChanges(result.data)
          setTotal(result.total)
          setHasMore(result.hasMore)
          setNextCursor(result.nextCursor)
          knownIdsRef.current = new Set(result.data.map((c) => c.id))
        })
        .catch(() => setPollFailed(true))

      startPolling(provider, winSince)
    } else {
      startPolling(provider, since)
    }
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
    setSyncedToWindow(true)

    const winSince = windowToSince(win)

    // Fetch new summary for the selected window
    fetchSummary(win, provider)
      .then(setSummary)
      .catch(() => {/* summary failure is non-critical */})

    // Sync the live feed to the same time window
    generationRef.current++
    const gen = generationRef.current

    setSince(winSince)
    knownIdsRef.current = new Set()
    setNextCursor(null)
    setHasMore(false)
    setLoadingMore(false)
    router.push(buildUrl(provider, winSince), { scroll: false })

    fetchChangesPage(provider, winSince)
      .then((result) => {
        if (generationRef.current !== gen) return
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

    startPolling(provider, winSince)
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



  return (
    <div style={{ overflowX: "hidden" }}>
      {/* Section header */}
      <div
        className="animate-wireframe-fade"
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: "12px",
          marginBottom: "16px",
          animationDelay: "1.2s",
        }}
      >
        <div>
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
              {total} {total === 1 ? "change" : "changes"}
            </span>
          )}
          {pollFailed ? (
            <SiteBadge
              label="Paused"
              color="var(--muted)"
              bg="#f5f5f5"
              dot
            />
          ) : (
            <span
              className="font-orbitron"
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "4px",
                padding: "1px 6px",
                border: "none",
                color: "#ffffff",
                backgroundColor: "#10B981",
                flexShrink: 0,
                letterSpacing: "0.12em",
                fontSize: "0.65rem",
                lineHeight: 1.4,
                textTransform: "uppercase",
              }}
            >
              <span
                className={polling ? "animate-live" : ""}
                style={{
                  width: "5px",
                  height: "5px",
                  borderRadius: "50%",
                  backgroundColor: "rgba(255,255,255,0.7)",
                  display: "inline-block",
                }}
              />
              LIVE
            </span>
          )}
          <span className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
            polling every 60s
          </span>
        </div>
      </div>

      {/* Time window selector */}
      <div className="animate-wireframe-fade" style={{ animationDelay: "1.3s" }}>
        <TimeWindowSelector value={timeWindow} onChange={handleTimeWindowChange} />
      </div>

      {/* Dashboard: Heatmap + Leaderboard */}
      {/* Mobile: movers above, heatmap full-width below (CSS order flip) */}
      {/* Desktop: side-by-side grid, heatmap takes remaining space */}
      <div
        style={{ marginBottom: "24px", gap: "16px", animationDelay: "1.4s" }}
        className="dashboard-grid flex flex-col lg:grid lg:grid-cols-[1fr_280px] animate-wireframe-fade"
      >
        <div className="order-2 lg:order-1">
          <PriceHeatmap
            heatmap={summary?.heatmap ?? null}
            onCellClick={(prov) => applyFilter(prov, since)}
          />
        </div>
        <div className="order-1 lg:order-2">
          <BiggestMovers topMovers={summary?.top_movers ?? null} />
        </div>
      </div>

      {/* Feed section label */}
      <span
        className="font-orbitron text-xs tracking-widest animate-wireframe-fade"
        style={{
          color: "var(--dim)",
          display: "block",
          marginBottom: "12px",
          animationDelay: "1.5s",
        }}
      >
        LIVE FEED
      </span>

      {/* Filters */}
      <div
        className="animate-draw-border-box"
        style={{
          marginBottom: "16px",
          backgroundColor: "var(--surfaceLo)",
          "--draw-delay": "0.6s"
        } as React.CSSProperties}
      >
        <div className="animate-wireframe-fade flex flex-wrap gap:8px p-3" style={{ animationDelay: "1.3s", gap: "8px" }}>
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

        <DatePicker
          value={syncedToWindow ? "" : since}
          onChange={(val) => {
            setSyncedToWindow(false)
            applyFilter(provider, val)
          }}
          placeholder="Since date"
        />

        {(provider || !syncedToWindow) && (
          <button
            onClick={() => {
              setSyncedToWindow(true)
              const winSince = windowToSince(timeWindow)
              applyFilter("", winSince)
            }}
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
      </div>

      {/* Column header */}
      <div
        className="font-orbitron text-xs animate-draw-border-b-dk"
        style={{
          padding: "8px 16px",
          "--draw-delay": "0.7s"
        } as React.CSSProperties}
      >
        <div className="animate-wireframe-fade" style={{ display: "flex", gap: "12px", textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--dim)", animationDelay: "1.4s" }}>
          <span style={{ flex: 1 }}>Model</span>
          <span style={{ minWidth: "180px" }}>Price Change</span>
          <span style={{ minWidth: "64px", textAlign: "right" }}>Delta</span>
          <span style={{ minWidth: "72px", textAlign: "right" }}>Source</span>
        </div>
      </div>

      {/* Rows */}
      {changes.length === 0 ? (
        <EmptyState padding="80px 24px">No price changes yet.</EmptyState>
      ) : (
        <>
          {changes.map((change, i) => (
            <ChangeRow
              key={change.id}
              change={change}
              isNew={newIds.has(change.id)}
              index={i}
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
