import type { MetadataRoute } from "next"
import { getModelsPaginated, getProviders } from "@/lib/api"
import type { Model } from "@/lib/api"
import {
  MAX_COMPARE_MODELS,
  encodeSlugPath,
  selectSitemapModels,
} from "@/lib/sitemap"

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL || "https://llmrates.live"

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const now = new Date()

  // Omit synthetic lastmod values for static routes. Claiming that every page
  // changed whenever Google fetches the sitemap makes the signal meaningless.
  const staticRoutes: MetadataRoute.Sitemap = [
    { url: SITE_URL, changeFrequency: "weekly", priority: 1.0 },
    { url: `${SITE_URL}/models`, changeFrequency: "daily", priority: 0.9 },
    { url: `${SITE_URL}/compare`, changeFrequency: "weekly", priority: 0.85 },
    { url: `${SITE_URL}/calculator`, changeFrequency: "weekly", priority: 0.7 },
    { url: `${SITE_URL}/changes`, changeFrequency: "hourly", priority: 0.8 },
    { url: `${SITE_URL}/docs`, changeFrequency: "weekly", priority: 0.95 },
    { url: `${SITE_URL}/why`, changeFrequency: "monthly", priority: 0.6 },
    { url: `${SITE_URL}/pricing`, changeFrequency: "monthly", priority: 0.8 },
  ]

  let sitemapModels: Model[] = []
  let modelRoutes: MetadataRoute.Sitemap = []
  let compareRoutes: MetadataRoute.Sitemap = []
  let providerRoutes: MetadataRoute.Sitemap = []

  try {
    // The API sorts by most recently confirmed by default. Inspect at most
    // 1,000 records, then publish only a rolling 90-day, priced, canonical set.
    const allModels: Model[] = []
    for (let page = 1; page <= 5; page++) {
      const result = await getModelsPaginated({ page, per_page: 200 })
      allModels.push(...result.data)
      if (allModels.length >= result.total || result.data.length === 0) break
    }
    sitemapModels = selectSitemapModels(allModels, now)

    modelRoutes = sitemapModels.map((model) => ({
      url: `${SITE_URL}/models/${encodeSlugPath(model.slug)}`,
      lastModified: new Date(model.updated_at),
      changeFrequency: "daily" as const,
      priority: 0.6,
    }))

    // Forty-five current pair pages are enough to expose specific comparison
    // intent without flooding the sitemap with an O(n²) cross-product.
    const topModels = sitemapModels
      .filter((model) => !model.slug.includes("-vs-"))
      .slice(0, MAX_COMPARE_MODELS)
    for (let i = 0; i < topModels.length; i++) {
      for (let j = i + 1; j < topModels.length; j++) {
        const a = topModels[i]
        const b = topModels[j]
        compareRoutes.push({
          url: `${SITE_URL}/compare/${encodeSlugPath(a.slug)}-vs-${encodeSlugPath(b.slug)}`,
          lastModified: new Date(
            Math.max(Date.parse(a.updated_at), Date.parse(b.updated_at)),
          ),
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
    const activeProviderDates = new Map<string, number>()
    for (const model of sitemapModels) {
      activeProviderDates.set(
        model.provider,
        Math.max(activeProviderDates.get(model.provider) ?? 0, Date.parse(model.updated_at)),
      )
    }

    providerRoutes = providers
      .filter((provider) => activeProviderDates.has(provider.id))
      .map((provider) => ({
        url: `${SITE_URL}/providers/${encodeURIComponent(provider.id)}`,
        lastModified: new Date(activeProviderDates.get(provider.id)!),
        changeFrequency: "daily" as const,
        priority: 0.7,
      }))
  } catch (err) {
    console.error("[sitemap] Failed to fetch provider routes:", err)
  }

  return [...staticRoutes, ...providerRoutes, ...compareRoutes, ...modelRoutes]
}
