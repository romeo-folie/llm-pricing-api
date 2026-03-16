/**
 * /signup/verify — token verification landing page
 *
 * The magic-link URL format is:
 *   https://llmrates.live/auth/signup/verify?token=<raw_token>
 *
 * The Go backend handles that route directly (session cookie + redirect to
 * /signup/free?verified=1). This Next.js page exists only as a fallback in
 * case someone bookmarks /signup/verify or the proxy strips the /auth prefix.
 * It immediately redirects to the correct backend path.
 */
import { redirect } from "next/navigation"

interface Props {
  searchParams: Promise<{ token?: string | string[] }>
}

export default async function VerifyPage({ searchParams }: Props) {
  const params = await searchParams
  const rawToken = Array.isArray(params.token) ? params.token[0] : params.token
  const token = typeof rawToken === "string" ? rawToken.trim() : ""

  if (!token) {
    // No token supplied — redirect to the signup page instead of forwarding
    // an empty token to the backend (which would return an error anyway).
    redirect("/signup/free")
  }

  // Forward to the backend handler which owns the cookie + redirect logic.
  redirect(`/auth/signup/verify?token=${encodeURIComponent(token)}`)
}
