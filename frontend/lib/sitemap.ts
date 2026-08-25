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
  // Bare upstream IDs and deep provider resource paths (for example,
  // `fireworks_ai/accounts/fireworks/models/ssd-1b`) are still available in
  // the product, but are not useful standalone search landing pages. Keep
  // qualified region/tier variants eligible because they can be distinct SKUs.
  if (!slug || slug.includes(":") || slug.includes("~")) return false

  const segments = slug.split("/")
  if (segments.length < 2 || !segments.every((segment) => SAFE_SEGMENT.test(segment))) return false

  const accountsIndex = segments.indexOf("accounts")
  const modelsIndex = segments.indexOf("models", accountsIndex + 1)
  return accountsIndex < 0 || modelsIndex < 0 || modelsIndex === segments.length - 1
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
