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
