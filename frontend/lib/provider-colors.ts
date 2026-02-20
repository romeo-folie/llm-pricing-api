/** Shared provider color tokens used across change-related components. */
export const PROVIDER_COLORS: Record<string, { color: string; bg: string }> = {
  openai:    { color: "var(--accent)", bg: "var(--accentLt)" },
  anthropic: { color: "var(--blue)",   bg: "var(--blueLt)"   },
  google:    { color: "var(--blue)",   bg: "var(--blueLt)"   },
  mistral:   { color: "var(--green)",  bg: "var(--greenLt)"  },
  meta:      { color: "var(--purple)", bg: "var(--purpleLt)" },
}

/** Look up provider color tokens with a safe fallback for unknown providers. */
export function providerStyle(provider: string): { color: string; bg: string } {
  const key = provider.toLowerCase()
  return PROVIDER_COLORS[key] ?? { color: "var(--dim)", bg: "var(--surfaceLo)" }
}
