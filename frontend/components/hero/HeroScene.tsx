"use client"

import { useEffect, useRef, type CSSProperties } from "react"
import Image from "next/image"
import HeroFallback from "./HeroFallback"

interface HeroSceneProps {
  className?: string
  style?: CSSProperties
}

// ─── Inner component rendered only when model-viewer path is chosen ─────────
// Keeping this as a separate component ensures hooks are never registered when
// the env flag routes to HeroFallback (React rules prohibit conditional hooks).

function ModelViewerHero({ className, style }: HeroSceneProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    // Dynamically load @google/model-viewer from npm package dist.
    // Using the local package avoids the CSP `unsafe-inline` + CDN risk.
    let cancelled = false
    const script = document.createElement("script")
    script.type = "module"
    // Note: Next.js serves node_modules via the bundler; model-viewer self-registers
    // the custom element on import. We use next/script for client-only module loading
    // in the component tree, but for dynamic post-LCP injection we append directly.
    script.src = "/_next/static/chunks/model-viewer.js"
    // Fallback: if the bundled path is unavailable, try the npm package path
    script.onerror = () => {
      if (cancelled) return
      const fallback = document.createElement("script")
      fallback.type = "module"
      fallback.src = "/model-viewer.js"
      document.head.appendChild(fallback)
    }

    script.onload = () => {
      if (cancelled || !container) return
      if (container.querySelector("model-viewer") !== null) return

      const mv = document.createElement("model-viewer" as keyof HTMLElementTagNameMap)
      mv.setAttribute("src", "/hero.glb")
      mv.setAttribute("alt", "Interactive isometric 3D scene: server racks with floating LLM price tokens")
      mv.setAttribute("poster", "/hero.webp")
      mv.setAttribute("auto-rotate", "")
      mv.setAttribute("camera-controls", "")
      mv.setAttribute("shadow-intensity", "0")
      mv.setAttribute("environment-image", "neutral")
      mv.setAttribute("exposure", "1.2")
      mv.setAttribute("loading", "lazy")
      mv.setAttribute("reveal", "interaction")
      mv.setAttribute("aria-label", "Interactive 3D hero scene")
      mv.style.cssText =
        "position:absolute;inset:0;width:100%;height:100%;z-index:2;background:transparent;"
      container.appendChild(mv)
    }

    document.head.appendChild(script)

    return () => {
      cancelled = true
      // Cancel pending onload callback and remove the injected script tag
      script.onload = null
      script.onerror = null
      if (script.parentNode) script.parentNode.removeChild(script)
      // Remove the model-viewer element if it was added
      const mv = container.querySelector("model-viewer")
      if (mv) mv.remove()
    }
  }, [])

  return (
    <div
      ref={containerRef}
      className={className}
      style={{
        position: "relative",
        border: "1px solid var(--border)",
        backgroundColor: "var(--surfaceLo)",
        borderRadius: "2px",
        overflow: "hidden",
        minHeight: "360px",
        ...style,
      }}
    >
      {/* WebP poster: loads immediately above fold; serves as the LCP image */}
      <Image
        src="/hero.webp"
        alt="Isometric server rack scene with floating price tokens and data pipeline elements"
        fill
        priority
        sizes="(max-width: 768px) 100vw, 50vw"
        className="object-cover"
        style={{ zIndex: 1 }}
      />

      {/* model-viewer element is injected into containerRef via useEffect after LCP */}

      {/* Amber corner accent — isometric depth treatment, no box-shadow */}
      <div
        aria-hidden="true"
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          width: "3px",
          height: "40px",
          backgroundColor: "var(--accent)",
          zIndex: 3,
        }}
      />
      <div
        aria-hidden="true"
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          width: "40px",
          height: "3px",
          backgroundColor: "var(--accent)",
          zIndex: 3,
        }}
      />
    </div>
  )
}

// ─── Public export ────────────────────────────────────────────────────────────
// The env-flag check happens here, before any hooks are registered, so that
// we never violate the Rules of Hooks.

export default function HeroScene({ className, style }: HeroSceneProps) {
  const useLottie = process.env.NEXT_PUBLIC_USE_LOTTIE_HERO === "true"

  if (useLottie) {
    return <HeroFallback className={className} style={style} />
  }

  return <ModelViewerHero className={className} style={style} />
}
