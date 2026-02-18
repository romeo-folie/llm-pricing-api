"use client"

import { Suspense, useEffect, useRef, type CSSProperties } from "react"
import Image from "next/image"
import HeroFallback from "./HeroFallback"

interface HeroSceneProps {
  className?: string
  style?: CSSProperties
}

function ModelViewerScene({ className, style }: HeroSceneProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    // Dynamically import @google/model-viewer (lazy — after LCP)
    const script = document.createElement("script")
    script.type = "module"
    script.src = "https://unpkg.com/@google/model-viewer/dist/model-viewer.min.js"
    document.head.appendChild(script)

    script.onload = () => {
      // Create the <model-viewer> element imperatively to avoid JSX typing issues
      // with this web component in the TypeScript + Turbopack environment
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
      mv.style.cssText = "position:absolute;inset:0;width:100%;height:100%;z-index:2;background:transparent;"

      if (container.querySelector("model-viewer") === null) {
        container.appendChild(mv)
      }
    }

    return () => {
      // Cleanup: remove the element if it was added
      const mv = container.querySelector("model-viewer")
      if (mv) mv.remove()
    }
  }, [])

  if (process.env.NEXT_PUBLIC_USE_LOTTIE_HERO === "true") {
    return <HeroFallback className={className} style={style} />
  }

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
      {/* WebP poster: loads immediately (above fold), serves as LCP element */}
      <Image
        src="/hero.webp"
        alt="Isometric server rack scene with floating price tokens and data pipeline elements"
        fill
        priority
        sizes="(max-width: 768px) 100vw, 50vw"
        className="object-cover"
        style={{ zIndex: 1 }}
      />

      {/* model-viewer injected into containerRef via useEffect after LCP */}

      {/* Amber corner accent — isometric depth treatment */}
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

export default function HeroScene({ className, style }: HeroSceneProps) {
  return (
    <Suspense fallback={<HeroFallback className={className} style={style} />}>
      <ModelViewerScene className={className} style={style} />
    </Suspense>
  )
}
