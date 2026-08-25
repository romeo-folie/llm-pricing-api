/** Decode each catch-all route segment once before reconstructing an API slug. */
export function decodeSlugParts(parts: string[]): string {
  return parts
    .map((part) => {
      try {
        return decodeURIComponent(part)
      } catch {
        return part
      }
    })
    .join("/")
}

/** Encode a slash-delimited model slug without collapsing route segments. */
export function encodeSlugPath(slug: string): string {
  return slug.split("/").map(encodeURIComponent).join("/")
}

/**
 * Return the one public URL for an unordered two-model comparison.
 *
 * Comparisons present the same information regardless of the order in which
 * their models were selected. Sorting by the resolved, canonical model slugs
 * prevents `a-vs-b` and `b-vs-a` from becoming separate indexable pages.
 */
export function canonicalComparePath(a: string, b: string): string {
  const [first, second] = a <= b ? [a, b] : [b, a]
  return `/compare/${encodeSlugPath(first)}-vs-${encodeSlugPath(second)}`
}
