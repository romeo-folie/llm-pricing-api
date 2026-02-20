import type { MetadataRoute } from "next"
import { getModelsPaginated, getProviders } from "@/lib/api"
import type { Model } from "@/lib/api"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const now = new Date()

  const staticRoutes: MetadataRoute.Sitemap = [
    {
      url: SITE_URL,
      lastModified: now,
      changeFrequency: "weekly",
      priority: 1.0,
    },
    {
      url: `${SITE_URL}/models`,
      lastModified: now,
      changeFrequency: "daily",
      priority: 0.9,
    },
    {
      url: `${SITE_URL}/compare`,
      lastModified: now,
      changeFrequency: "weekly",
      priority: 0.7,
    },
    {
      url: `${SITE_URL}/calculator`,
      lastModified: now,
      changeFrequency: "weekly",
      priority: 0.7,
    },
    {
      url: `${SITE_URL}/changes`,
      lastModified: now,
      changeFrequency: "hourly",
      priority: 0.8,
    },
    {
      url: `${SITE_URL}/pricing`,
      lastModified: now,
      changeFrequency: "monthly",
      priority: 0.8,
    },
  ]

  let modelRoutes: MetadataRoute.Sitemap = []
  let providerRoutes: MetadataRoute.Sitemap = []

  let compareRoutes: MetadataRoute.Sitemap = []

  try {
    // Fetch up to 1000 models across at most 5 pages (5 × 200) rather than
    // calling getModels() which now eagerly paginates the full catalogue.
    const allModels: Model[] = []
    for (let pg = 1; pg <= 5; pg++) {
      const result = await getModelsPaginated({ page: pg, per_page: 200 })
      allModels.push(...result.data)
      if (allModels.length >= result.total || result.data.length === 0) break
    }
    const models = allModels.slice(0, 1000)

    modelRoutes = models.map((model) => ({
      url: `${SITE_URL}/models/${model.slug}`,
      lastModified: model.updated_at ? new Date(model.updated_at) : undefined,
      changeFrequency: "daily" as const,
      priority: 0.6,
    }))

    // Pre-generate comparison pages for the top 15 models (~105 pairs).
    // Skip any model whose slug contains "-vs-" to avoid malformed URLs.
    const topModels = models.slice(0, 15).filter((m) => !m.slug.includes("-vs-"))
    for (let i = 0; i < topModels.length; i++) {
      for (let j = i + 1; j < topModels.length; j++) {
        const a = topModels[i]
        const b = topModels[j]
        compareRoutes.push({
          url: `${SITE_URL}/compare/${a.slug}-vs-${b.slug}`,
          lastModified: now,
          changeFrequency: "daily" as const,
          priority: 0.65,
        })
      }
    }
  } catch (err) {
    console.error("[sitemap] Failed to fetch model routes:", err)
  }

  try {
    const providers = await getProviders()
    providerRoutes = providers.map((p) => ({
      url: `${SITE_URL}/providers/${encodeURIComponent(p.id)}`,
      lastModified: now,
      changeFrequency: "daily" as const,
      priority: 0.7,
    }))
  } catch (err) {
    console.error("[sitemap] Failed to fetch provider routes:", err)
  }

  return [...staticRoutes, ...providerRoutes, ...compareRoutes, ...modelRoutes]
}
