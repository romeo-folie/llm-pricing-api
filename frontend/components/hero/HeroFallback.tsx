"use client"

import dynamic from "next/dynamic"
import type { CSSProperties } from "react"

// Lazy-load LottiePlayer to avoid SSR issues with the animation library
const Player = dynamic(
  () => import("@lottiefiles/react-lottie-player").then((m) => m.Player),
  { ssr: false },
)

// LottieFiles "Isometric AI Animation" hosted JSON
const LOTTIE_URL =
  "https://assets10.lottiefiles.com/packages/lf20_ztUY20dte4.json"

interface HeroFallbackProps {
  className?: string
  style?: CSSProperties
}

export default function HeroFallback({ className, style }: HeroFallbackProps) {
  return (
    <div
      className={className}
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        border: "1px solid var(--border)",
        backgroundColor: "var(--surfaceLo)",
        borderRadius: "2px",
        ...style,
      }}
      aria-label="Isometric AI animation hero"
      role="img"
    >
      <div
        style={{
          position: "relative",
          width: "100%",
          maxWidth: "600px",
          aspectRatio: "3/2",
        }}
      >
        {/* LottieFiles player — client-side only, loaded by URL */}
        <Player
          src={LOTTIE_URL}
          autoplay
          loop
          style={{ width: "100%", height: "100%" }}
        />

        {/* Isometric grid overlay matching the design spec */}
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
              id="iso-grid"
              x="0"
              y="0"
              width="40"
              height="40"
              patternUnits="userSpaceOnUse"
              patternTransform="rotate(-30) skewX(20)"
            >
              <path
                d="M 40 0 L 0 0 0 40"
                fill="none"
                stroke="var(--borderDk)"
                strokeWidth="0.5"
              />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#iso-grid)" />
        </svg>
      </div>
    </div>
  )
}
