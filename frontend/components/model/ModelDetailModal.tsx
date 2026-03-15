"use client"

import { useEffect, useState, useCallback } from "react"
import { useRouter, useSearchParams, usePathname } from "next/navigation"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import PriceHistoryChart from "./PriceHistoryChart"
import { trackViewModelDetail, trackAddToCompare } from "@/lib/analytics"
import type { Model, PriceHistoryEntry } from "@/lib/api"
import { formatPrice, formatContext, formatAge, formatSourceName } from "@/lib/format"

interface ModalData {
  model: Model
  history: PriceHistoryEntry[]
}

function Badge({ label, color, bg }: { label: string; color: string; bg: string }) {
  return (
    <span
      className="font-orbitron text-xs"
      style={{
        padding: "2px 8px",
        border: `1px solid ${color}`,


        color,
        backgroundColor: bg,
      }}
    >
      {label}
    </span>
  )
}

function ConfidenceBadge({ confidence }: { confidence: "high" | "medium" | "low" }) {
  if (confidence === "high")   return <Badge label="HIGH"   color="var(--green)"  bg="var(--greenLt)"  />
  if (confidence === "medium") return <Badge label="MEDIUM" color="var(--accent)" bg="var(--accentLt)" />
  return                                <Badge label="LOW"   color="var(--red)"    bg="var(--redLt)"    />
}

export default function ModelDetailModal() {
  const router      = useRouter()
  const pathname    = usePathname()
  const searchParams = useSearchParams()
  const modelId     = searchParams.get("model")

  const [data,    setData]    = useState<ModalData | null>(null)
  const [loading, setLoading] = useState(false)
  const [error,   setError]   = useState<string | null>(null)

  const fetchData = useCallback(async (id: string) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/model/${encodeURIComponent(id)}`)
      if (!res.ok) throw new Error(`Error ${res.status}`)
      const json = (await res.json()) as ModalData
      setData(json)
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load model")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (modelId) {
      setData(null)
      fetchData(modelId)
      trackViewModelDetail(modelId)
    }
  }, [modelId, fetchData])

  function closeModal() {
    const params = new URLSearchParams(searchParams.toString())
    params.delete("model")
    const qs = params.toString()
    router.push(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
  }

  function addToCompare() {
    if (!modelId) return
    trackAddToCompare(modelId)
    // Close the modal first by removing the `model` query param, then navigate
    // to /compare — keeps navigation SPA and avoids losing in-memory state.
    const params = new URLSearchParams(searchParams.toString())
    params.delete("model")
    const qs = params.toString()
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    router.push(`/compare?models=${encodeURIComponent(modelId)}`)
  }

  const isOpen = !!modelId

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open) closeModal() }}>
      <DialogContent
        style={{
          maxWidth: "680px",
          maxHeight: "90vh",
          overflowY: "auto",
          backgroundColor: "var(--surface)",
          border: "1px solid var(--borderDk)",
  

          padding: "24px",
        }}
      >
        {loading && (
          <div className="font-outfit text-sm" style={{ color: "var(--muted)", textAlign: "center", padding: "48px" }}>
            Loading…
          </div>
        )}

        {error && (
          <div className="font-outfit text-sm" style={{ color: "var(--red)", textAlign: "center", padding: "48px" }}>
            {error}
          </div>
        )}

        {data && !loading && (
          <>
            <DialogHeader>
              <DialogTitle className="font-outfit" style={{ color: "var(--ink)", fontSize: "18px", fontWeight: 700 }}>
                {data.model.name}
              </DialogTitle>
              <DialogDescription className="sr-only">
                Pricing details for {data.model.name} by {data.model.provider}
              </DialogDescription>
              <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginTop: "8px" }}>
                <Badge label={data.model.provider} color="var(--blue)"   bg="var(--blueLt)"   />
                <Badge label={data.model.modality} color="var(--purple)" bg="var(--purpleLt)" />
                <ConfidenceBadge confidence={data.model.trust.confidence} />
              </div>
            </DialogHeader>

            {/* Prices */}
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr",
                gap: "12px",
                marginTop: "20px",
              }}
            >
              {[
                { label: "Input / 1M tokens",  value: formatPrice(data.model.input_price_per_m)  },
                { label: "Output / 1M tokens", value: formatPrice(data.model.output_price_per_m) },
              ].map(({ label, value }) => (
                <div
                  key={label}
                  style={{
                    padding: "12px",
                    border: "1px solid var(--border)",
            

                    backgroundColor: "var(--surfaceLo)",
                  }}
                >
                  <div className="font-outfit text-xs" style={{ color: "var(--dim)", marginBottom: "4px" }}>{label}</div>
                  <div className="font-orbitron text-lg" style={{ color: "var(--ink)" }}>{value}</div>
                </div>
              ))}
            </div>

            {/* Meta */}
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr 1fr",
                gap: "8px",
                marginTop: "12px",
                padding: "12px",
                border: "1px solid var(--border)",
                backgroundColor: "var(--surfaceLo)",
        

              }}
            >
              <div>
                <div className="font-outfit text-xs" style={{ color: "var(--dim)" }}>Context</div>
                <div className="font-orbitron text-sm" style={{ color: "var(--text)" }}>{formatContext(data.model.context_window)}</div>
              </div>
              <div>
                <div className="font-outfit text-xs" style={{ color: "var(--dim)" }}>Source</div>
                <div className="font-outfit text-sm" style={{ color: "var(--text)" }}>
                  {formatSourceName(data.model.trust.source)}
                  {data.model.underlying_provider && (
                    <span style={{ color: "var(--muted)", marginLeft: "4px" }}>
                      via {data.model.underlying_provider.charAt(0).toUpperCase() + data.model.underlying_provider.slice(1)}
                    </span>
                  )}
                </div>
              </div>
              <div>
                <div className="font-outfit text-xs" style={{ color: "var(--dim)" }}>Updated</div>
                <div className="font-outfit text-sm" style={{ color: "var(--text)" }}>{formatAge(data.model.trust.age_hours)}</div>
              </div>
            </div>

            {/* Trust strip */}
            <div
              className="font-outfit text-xs"
              style={{
                marginTop: "8px",
                padding: "8px 12px",
                border: "1px solid var(--border)",
                borderLeft: "3px solid var(--accent)",
                backgroundColor: "var(--accentLt)",
                color: "var(--accentDk)",
                display: "flex",
                gap: "16px",
                flexWrap: "wrap",
              }}
            >
              <span>Confirmed: {new Date(data.model.trust.confirmed_at).toLocaleDateString()}</span>
              <span>Change velocity: {data.model.trust.change_velocity.toFixed(2)}/wk</span>
              {data.model.trust.age_hours >= 24 && (
                <span style={{ color: "var(--red)", fontWeight: 600 }}>⚠ Data may be stale</span>
              )}
            </div>

            {/* Price history */}
            <div style={{ marginTop: "20px" }}>
              <h3 className="font-orbitron text-xs" style={{ color: "var(--dim)", marginBottom: "12px", textTransform: "uppercase", letterSpacing: "0.1em" }}>
                Price History
              </h3>
              <PriceHistoryChart history={data.history} />
            </div>

            {/* Actions */}
            <div style={{ marginTop: "20px", display: "flex", gap: "8px" }}>
              <button
                onClick={addToCompare}
                className="font-outfit text-sm"
                style={{
                  padding: "8px 16px",
                  border: "1px solid var(--accent)",
          

                  backgroundColor: "var(--accent)",
                  color: "white",
                  cursor: "pointer",
                }}
              >
                Add to Compare
              </button>
              <a
                href={`/models/${data.model.slug}`}
                className="font-outfit text-sm"
                style={{
                  padding: "8px 16px",
                  border: "1px solid var(--border)",
          

                  backgroundColor: "var(--surface)",
                  color: "var(--muted)",
                  textDecoration: "none",
                  display: "inline-flex",
                  alignItems: "center",
                }}
              >
                Full page ↗
              </a>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
