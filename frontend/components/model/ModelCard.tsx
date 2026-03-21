"use client"

import { useRouter, useSearchParams } from "next/navigation"
import type { Model } from "@/lib/api"
import { formatPrice, formatContext } from "@/lib/format"
import { safeJsonLd } from "@/lib/utils"

interface ModelCardProps {
  model: Model
}

function ConfidenceDot({ confidence }: { confidence: "high" | "medium" | "low" }) {
  const color =
    confidence === "high"   ? "var(--green)"  :
    confidence === "medium" ? "var(--accent)" : "var(--red)"
  const label =
    confidence === "high"   ? "High confidence"   :
    confidence === "medium" ? "Medium confidence" : "Low confidence"
  return (
    <span
      aria-label={label}
      title={label}
      style={{
        display: "inline-block",
        width: "8px",
        height: "8px",
        borderRadius: "50%",
        backgroundColor: color,
        flexShrink: 0,
      }}
    />
  )
}

export default function ModelCard({ model }: ModelCardProps) {
  const router      = useRouter()
  const searchParams = useSearchParams()

  function openModal() {
    const params = new URLSearchParams(searchParams.toString())
    params.set("model", model.slug)
    router.push(`?${params.toString()}`, { scroll: false })
  }

  const productSchema = {
    "@context": "https://schema.org/",
    "@type": "Product",
    "name": model.name,
    "description": `${model.provider} ${model.name} model with ${formatContext(model.context_window)} context window.`,
    "brand": {
      "@type": "Brand",
      "name": model.provider
    },
    "offers": {
      "@type": "Offer",
      "priceCurrency": "USD",
      "price": model.input_price_per_m,
      "description": `Input price: ${formatPrice(model.input_price_per_m)}, Output price: ${formatPrice(model.output_price_per_m)} per 1M tokens.`
    }
  };

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: safeJsonLd(productSchema) }}
      />
      <button
        onClick={openModal}
      className="w-full text-left"
      style={{
        display: "flex",
        alignItems: "center",
        gap: "12px",
        padding: "12px 16px",
        backgroundColor: "transparent",
        borderBottom: "1px solid var(--border)",
        cursor: "pointer",
        transition: "background-color 0.12s",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.backgroundColor = "var(--surface)"
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor = "transparent"
      }}
    >
      {/* Confidence dot */}
      <ConfidenceDot confidence={model.trust.confidence} />

      {/* Model name + provider */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="font-outfit text-sm" style={{ color: "var(--ink)", fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
          {model.name}
        </div>
        <div className="font-outfit text-xs" style={{ color: "var(--muted)" }}>
          {model.provider} · {model.modality}
        </div>
      </div>

      {/* LIVE badge */}
      <span
        className="font-orbitron text-xs"
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
        }}
      >
        <span className="animate-live" style={{ width: "5px", height: "5px", borderRadius: "50%", backgroundColor: "rgba(255,255,255,0.7)", display: "inline-block" }} />
        LIVE
      </span>

      {/* Context window */}
      <span
        className="font-orbitron text-xs"
        style={{ color: "var(--dim)", flexShrink: 0, minWidth: "36px", textAlign: "right" }}
      >
        {formatContext(model.context_window)}
      </span>

      {/* Prices */}
      <div style={{ flexShrink: 0, textAlign: "right", minWidth: "100px" }}>
        <div className="font-orbitron text-xs" style={{ color: "var(--text)" }}>
          {formatPrice(model.input_price_per_m)}
          <span className="font-outfit" style={{ color: "var(--dim)", fontSize: "10px" }}> in</span>
        </div>
        <div className="font-orbitron text-xs" style={{ color: "var(--muted)" }}>
          {formatPrice(model.output_price_per_m)}
          <span className="font-outfit" style={{ color: "var(--dim)", fontSize: "10px" }}> out</span>
        </div>
      </div>
    </button>
    </>
  )
}
