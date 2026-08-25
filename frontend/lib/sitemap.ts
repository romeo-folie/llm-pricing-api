import type { Model } from "@/lib/api"
import { encodeSlugPath } from "@/lib/routes"

export { encodeSlugPath } from "@/lib/routes"

export const SITEMAP_MODEL_MAX_AGE_DAYS = 90
export const MAX_SITEMAP_MODELS = 500
export const MAX_COMPARE_MODELS = 10

const DAY_MS = 24 * 60 * 60 * 1000
const SAFE_SEGMENT = /^[A-Za-z0-9][A-Za-z0-9._-]*$/

/** Curate discovery URLs instead of publishing every catalogue record. */
export function isCanonicalPublicSlug(slug: string): boolean {
  // Colon variants currently do not resolve as frontend pages, while tilde
  // records are aliases rather than canonical public model identities.
  if (!slug || slug.includes(":") || slug.includes("~")) return false

  return slug.split("/").every((segment) => SAFE_SEGMENT.test(segment))
}

export function selectSitemapModels(
  models: Model[],
  now = new Date(),
): Model[] {
  const oldestAllowed = now.getTime() - SITEMAP_MODEL_MAX_AGE_DAYS * DAY_MS
  const newestAllowed = now.getTime() + DAY_MS
  const seen = new Set<string>()

  return models
    .filter((model) => {
      if (!isCanonicalPublicSlug(model.slug)) return false
      if (model.input_price_per_m <= 0 && model.output_price_per_m <= 0) return false

      const updatedAt = Date.parse(model.updated_at)
      if (!Number.isFinite(updatedAt) || updatedAt < oldestAllowed || updatedAt > newestAllowed) {
        return false
      }

      if (seen.has(model.slug)) return false
      seen.add(model.slug)
      return true
    })
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .slice(0, MAX_SITEMAP_MODELS)
}
