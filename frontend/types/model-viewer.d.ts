// Type declarations for the @google/model-viewer web component
// https://modelviewer.dev/

import type { CSSProperties, DetailedHTMLProps, HTMLAttributes } from "react"

interface ModelViewerAttributes {
  src?: string
  alt?: string
  poster?: string
  autoplay?: boolean
  "auto-rotate"?: boolean
  "camera-controls"?: boolean
  "shadow-intensity"?: string
  "environment-image"?: string
  exposure?: string
  loading?: "auto" | "lazy" | "eager"
  reveal?: "auto" | "interaction" | "manual"
  style?: CSSProperties
  className?: string
  "aria-label"?: string
}

declare global {
  namespace JSX {
    interface IntrinsicElements {
      "model-viewer": DetailedHTMLProps<
        HTMLAttributes<HTMLElement> & ModelViewerAttributes,
        HTMLElement
      >
    }
  }
}

export {}
