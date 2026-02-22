import { NextResponse } from "next/server";
import { cookieName } from "@/lib/session";

export const dynamic = "force-dynamic";

/**
 * POST /api/account/signout
 *
 * Clears the account session cookie by setting Max-Age=0, then redirects to
 * the account page (which will show the key-input form).
 */
export async function POST() {
  // BASE_URL (server-only, no NEXT_PUBLIC_ prefix) avoids unnecessarily
  // inlining the value into client-side bundles.
  const response = NextResponse.redirect(new URL("/account", process.env.BASE_URL || "http://localhost:3000"));
  response.headers.set(
    "Set-Cookie",
    `${cookieName()}=; HttpOnly; SameSite=Lax; Max-Age=0; Path=/`
  );
  return response;
}
