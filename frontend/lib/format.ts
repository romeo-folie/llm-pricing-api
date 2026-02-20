/** Format a per-million-token price with 4 decimal places. */
export function formatPrice(p: number): string {
  return `$${p.toFixed(4)}`
}

/** Format a context window size as human-readable (e.g. 128K, 1M). */
export function formatContext(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`
  return String(n)
}

/** Format a currency amount with 2 decimal places. */
export function formatCurrency(n: number): string {
  return n.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 })
}

/** Format a percentage delta with sign prefix. */
export function formatDelta(pct: number): string {
  const sign = pct >= 0 ? "+" : ""
  return `${sign}${pct.toFixed(1)}%`
}

/** Format a trust age in hours as a relative time string. */
export function formatAge(hours: number): string {
  if (hours < 1) return "< 1h ago"
  if (hours < 24) return `${Math.round(hours)}h ago`
  return `${Math.round(hours / 24)}d ago`
}

/** Map raw backend source identifiers to human-readable display names. */
export function formatSourceName(source: string): string {
  const map: Record<string, string> = {
    openrouter: "OpenRouter",
    litellm: "LiteLLM",
    huggingface_inference_providers: "Hugging Face",
  }
  return map[source] ?? source
}

/**
 * Format a provider identifier (as stored in the DB, e.g. "openai", "x-ai")
 * into a human-readable display name for use in headings and metadata.
 */
export function formatProviderName(slug: string): string {
  const known: Record<string, string> = {
    openai:          "OpenAI",
    anthropic:       "Anthropic",
    google:          "Google",
    "x-ai":          "xAI",
    xai:             "xAI",
    mistralai:       "Mistral AI",
    mistral:         "Mistral AI",
    cohere:          "Cohere",
    meta:            "Meta",
    "meta-llama":    "Meta Llama",
    amazon:          "Amazon",
    azure:           "Azure",
    groq:            "Groq",
    together:        "Together AI",
    togetherai:      "Together AI",
    perplexity:      "Perplexity",
    deepseek:        "DeepSeek",
    "01-ai":         "01.AI",
    huggingface:     "Hugging Face",
    ollama:          "Ollama",
  }
  if (known[slug]) return known[slug]
  // Fallback: capitalise each word
  return slug
    .split(/[-_\s]/)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ")
}

/** Format an ISO timestamp as a relative time string. */
export function formatRelative(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return "just now"
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}
