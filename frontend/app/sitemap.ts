import type { MetadataRoute } from "next"
import { getModels } from "@/lib/api"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmprice.dev"

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const staticRoutes: MetadataRoute.Sitemap = [
    {
      url: SITE_URL,
      changeFrequency: "monthly",
      priority: 1.0,
    },
    {
      url: `${SITE_URL}/models`,
      changeFrequency: "daily",
      priority: 0.9,
    },
    {
      url: `${SITE_URL}/compare`,
      changeFrequency: "weekly",
      priority: 0.7,
    },
    {
      url: `${SITE_URL}/calculator`,
      changeFrequency: "weekly",
      priority: 0.7,
    },
    {
      url: `${SITE_URL}/changes`,
      changeFrequency: "hourly",
      priority: 0.8,
    },
    {
      url: `${SITE_URL}/pricing`,
      changeFrequency: "monthly",
      priority: 0.8,
    },
  ]

  let modelRoutes: MetadataRoute.Sitemap = []
  try {
    const models = await getModels()
    // Cap at 1000 models to avoid sitemap size limits
    modelRoutes = models.slice(0, 1000).map((model) => ({
      url: `${SITE_URL}/models/${encodeURIComponent(model.id)}`,
      lastModified: model.updated_at ? new Date(model.updated_at) : undefined,
      changeFrequency: "daily" as const,
      priority: 0.6,
    }))
  } catch (err) {
    console.error("[sitemap] Failed to fetch model routes:", err)
  }

  return [...staticRoutes, ...modelRoutes]
}
