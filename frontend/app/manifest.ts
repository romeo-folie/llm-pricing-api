import type { MetadataRoute } from "next"

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "LLMRates — AI Model Pricing API",
    short_name: "LLMRates",
    description:
      "Compare AI model pricing across providers. Reconciled LLM token costs with full price history and agent-optimized APIs.",
    start_url: "/",
    display: "standalone",
    background_color: "#F2EDE8",
    theme_color: "#107E72",
    icons: [
      {
        src: "/icon-192.png",
        sizes: "192x192",
        type: "image/png",
      },
      {
        src: "/icon-512.png",
        sizes: "512x512",
        type: "image/png",
      },
    ],
  }
}
