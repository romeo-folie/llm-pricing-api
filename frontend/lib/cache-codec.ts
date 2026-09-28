import "server-only"
import { gunzipSync, gzipSync } from "node:zlib"

// Next.js refuses data-cache entries over 2MB (measured as JSON length) and in
// production drops them with only a warning, so large payloads are stored as
// gzip + base64 strings.

export function encodeCachePayload(value: unknown): string {
  return gzipSync(Buffer.from(JSON.stringify(value), "utf8")).toString("base64")
}

export function decodeCachePayload<T>(encoded: string): T {
  return JSON.parse(gunzipSync(Buffer.from(encoded, "base64")).toString("utf8")) as T
}
