import { ImageResponse } from "next/og"

export const runtime = "edge"
export const alt = "LLMRates — Compare AI Model Pricing Across Providers"
export const size = { width: 1200, height: 630 }
export const contentType = "image/png"

export default function OGImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          alignItems: "center",
          backgroundColor: "#F2EDE8",
          border: "4px solid #DDD7D0",
          fontFamily: "system-ui, sans-serif",
        }}
      >
        {/* Top decorative bar */}
        <div
          style={{
            position: "absolute",
            top: 0,
            left: 0,
            right: 0,
            height: "6px",
            backgroundColor: "#107E72",
          }}
        />

        {/* Bracket decoration */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "16px",
            marginBottom: "24px",
          }}
        >
          <span style={{ fontSize: "14px", color: "#A8A29E", letterSpacing: "0.2em" }}>
            [ RECONCILED LLM PRICING ]
          </span>
        </div>

        {/* Title */}
        <div
          style={{
            fontSize: "64px",
            fontWeight: 800,
            color: "#1E293B",
            display: "flex",
            alignItems: "center",
            gap: "12px",
          }}
        >
          <span>LLM</span>
          <span style={{ color: "#107E72" }}>Rates</span>
        </div>

        {/* Subtitle */}
        <div
          style={{
            fontSize: "22px",
            color: "#78716C",
            marginTop: "16px",
            textAlign: "center",
            maxWidth: "700px",
          }}
        >
          Compare AI model pricing. Full price history. Agent-optimized APIs.
        </div>

        {/* Stats bar */}
        <div
          style={{
            display: "flex",
            gap: "48px",
            marginTop: "40px",
            padding: "16px 32px",
            borderTop: "1px solid #DDD7D0",
            borderBottom: "1px solid #DDD7D0",
          }}
        >
          {[
            { label: "Models", value: "340+" },
            { label: "Providers", value: "7" },
            { label: "Sources", value: "3" },
          ].map((stat) => (
            <div
              key={stat.label}
              style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: "4px",
              }}
            >
              <span style={{ fontSize: "28px", fontWeight: 700, color: "#107E72" }}>
                {stat.value}
              </span>
              <span style={{ fontSize: "12px", color: "#A8A29E", letterSpacing: "0.1em" }}>
                {stat.label.toUpperCase()}
              </span>
            </div>
          ))}
        </div>

        {/* URL */}
        <div
          style={{
            position: "absolute",
            bottom: "24px",
            fontSize: "14px",
            color: "#A8A29E",
          }}
        >
          llmrates.live
        </div>
      </div>
    ),
    { ...size },
  )
}
