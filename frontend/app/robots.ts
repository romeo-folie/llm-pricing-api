import type { MetadataRoute } from "next"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: [
        "/", 
        "/openapi.json", 
        "/llms.txt", 
        "/.well-known/ai-plugin.json"
      ],
      disallow: ["/api/"],
    },
    sitemap: `${SITE_URL}/sitemap.xml`,
  }
}
