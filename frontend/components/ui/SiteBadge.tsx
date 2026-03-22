"use client"

import * as React from "react"
import { cn } from "@/lib/utils"

interface SiteBadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  label: string
  color?: string
  bg?: string
  /** If true, the badge is filled with the color and text is white (or similar) */
  filled?: boolean
  /** Optional icon or dot on the left */
  dot?: boolean
  dotClassName?: string
}

export function SiteBadge({
  label,
  color = "var(--dim)",
  bg = "var(--surfaceLo)",
  filled = false, // Ignored now, enforcing the "live scroll" look
  dot = false,
  dotClassName = "",
  className,
  style,
  ...props
}: SiteBadgeProps) {
  // Map specific ones to keep proper branding capitalization
  const MAP: Record<string, string> = {
    openai: "OpenAI",
    anthropic: "Anthropic",
    google: "Google",
    mistral: "Mistral",
    meta: "Meta",
    cohere: "Cohere",
    deepseek: "DeepSeek",
    groq: "Groq",
    huggingface: "Hugging Face",
    openrouter: "OpenRouter",
  }
  
  const displayLabel = MAP[label.toLowerCase()] || (label.charAt(0).toUpperCase() + label.slice(1).toLowerCase())

  const finalStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: "5px",
    padding: "3px 9px",
    borderRadius: "3px",
    border: "none",
    color: color,
    backgroundColor: bg,
    fontSize: "0.68rem",
    fontWeight: 600,
    lineHeight: 1,
    whiteSpace: "nowrap",
    ...style,
  }

  return (
    <span
      className={cn("font-outfit", className)}
      style={finalStyle}
      {...props}
    >
      {dot && (
        <span
          className={cn("animate-live", dotClassName)}
          style={{
            width: "5px",
            height: "5px",
            borderRadius: "50%",
            backgroundColor: color,
            display: "inline-block",
          }}
        />
      )}
      {displayLabel}
    </span>
  )
}
