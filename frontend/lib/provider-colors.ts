/** Shared provider color tokens used across change-related components. */
export const PROVIDER_COLORS: Record<string, { color: string; bg: string }> = {
  openai:    { color: "#1a6b2e", bg: "#e8f5e9" },
  anthropic: { color: "#b45309", bg: "#fdf3ec" },
  google:    { color: "#1a56db", bg: "#e8f0fe" },
  mistral:   { color: "#854d0e", bg: "#fef9c3" },
  meta:      { color: "#1e40af", bg: "#dbeafe" },
  cohere:    { color: "#6d28d9", bg: "#ede9fe" },
  deepseek:  { color: "#9d174d", bg: "#fce7f3" },
  groq:      { color: "#e11d48", bg: "#fff1f2" },
}

/** Look up provider color tokens with a safe fallback for unknown providers. */
export function providerStyle(provider: string): { color: string; bg: string } {
  const key = provider.toLowerCase()
  return PROVIDER_COLORS[key] ?? { color: "var(--dim)", bg: "var(--surfaceLo)" }
}
