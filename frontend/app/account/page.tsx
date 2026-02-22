import type { Metadata } from "next";
import { cookies } from "next/headers";
import { verifySession, cookieName } from "@/lib/session";
import KeyInputForm from "./KeyInputForm";
import AccountDashboard from "./AccountDashboard";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Account Dashboard — LLMRates",
  description: "View your API plan, key, and daily usage stats.",
  robots: { index: false, follow: false },
};

/**
 * Account dashboard page.
 *
 * Server Component — reads the session cookie on the server to decide which
 * UI to render. No client-side auth redirect; the cookie is HttpOnly so the
 * JS bundle never sees its value.
 *
 * - No session cookie  →  KeyInputForm (enter your API key)
 * - Valid session      →  AccountDashboard (3-card layout with live usage)
 */
export default async function AccountPage() {
  const jar = await cookies();
  const cookie = jar.get(cookieName());
  const session = cookie ? verifySession(cookie.value) : null;

  return (
    <main
      style={{
        minHeight: "100vh",
        background: "var(--bg)",
        padding: "4rem 1.5rem 6rem",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: "3rem",
      }}
    >
      {/* header */}
      <header
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: "0.75rem",
          maxWidth: "720px",
          textAlign: "center",
          animation: "account-rise 0.4s ease both",
        }}
      >
        <style>{`
          @keyframes account-rise {
            from { opacity: 0; transform: translateY(8px); }
            to   { opacity: 1; transform: translateY(0); }
          }
        `}</style>

        <p
          style={{
            fontFamily: "var(--font-orbitron, monospace)",
            fontSize: "0.6rem",
            letterSpacing: "0.18em",
            textTransform: "uppercase",
            color: "var(--accent)",
            margin: 0,
          }}
        >
          [ Account ]
        </p>

        <h1
          style={{
            fontFamily: "var(--font-orbitron, monospace)",
            fontSize: "clamp(1.4rem, 4vw, 2rem)",
            fontWeight: 700,
            letterSpacing: "-0.02em",
            color: "var(--ink)",
            margin: 0,
          }}
        >
          {session ? "Your API Access" : "Access Dashboard"}
        </h1>

        {!session && (
          <p
            style={{
              fontSize: "0.9rem",
              color: "var(--muted)",
              margin: 0,
              lineHeight: 1.6,
            }}
          >
            Enter your LLMRates API key to view your plan, usage, and manage your
            subscription.
          </p>
        )}
      </header>

      {/* content */}
      {session ? (
        <div
          style={{
            width: "100%",
            maxWidth: "960px",
            display: "flex",
            flexDirection: "column",
            gap: "2rem",
          }}
        >
          <AccountDashboard
            session={session}
            checkoutDev={process.env.NEXT_PUBLIC_LS_CHECKOUT_DEV}
            checkoutPro={process.env.NEXT_PUBLIC_LS_CHECKOUT_PRO}
          />

          {/* sign-out link */}
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <form action="/api/account/signout" method="POST">
              <button
                type="submit"
                style={{
                  fontFamily: "var(--font-orbitron, monospace)",
                  fontSize: "0.6rem",
                  letterSpacing: "0.1em",
                  textTransform: "uppercase",
                  background: "none",
                  border: "none",
                  color: "var(--dim)",
                  cursor: "pointer",
                  padding: 0,
                  textDecoration: "underline",
                  textDecorationColor: "var(--border)",
                }}
              >
                Sign out
              </button>
            </form>
          </div>
        </div>
      ) : (
        <div
          style={{
            background: "var(--surface)",
            border: "1px solid var(--border)",
            borderRadius: "6px",
            padding: "2.5rem",
            width: "100%",
            maxWidth: "520px",
            animation: "account-rise 0.45s ease both",
            animationDelay: "80ms",
          }}
        >
          <KeyInputForm />
        </div>
      )}
    </main>
  );
}
