"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useState, useRef } from "react"
import Lottie, { LottieRefCurrentProps } from "lottie-react"
import greenMoneyAnim from "@/public/animations/green-money.json"
import { cn } from "@/lib/utils"

const NAV_LINKS = [
  { href: "/models", label: "Models" },
  { href: "/compare", label: "Compare" },
  { href: "/calculator", label: "Calculator" },
  { href: "/changes", label: "Changes" },
  { href: "/docs", label: "Docs" },
  { href: "/pricing", label: "Features" },
] as const

export default function Nav() {
  const pathname = usePathname()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [animDone, setAnimDone] = useState(false)
  const lottieRef = useRef<LottieRefCurrentProps>(null)

  return (
    <header
      className="sticky top-0 z-50 w-full"
      style={{ backgroundColor: "var(--bg)" }}
    >
      <div className="mx-auto max-w-[1280px] border-x border-y" style={{ borderColor: "var(--border)" }}>
        <div className="grid h-13 grid-cols-[auto_1fr_auto] items-center px-6 md:h-14 md:px-8" style={{ position: "relative" }}>

          {/* Logo */}
          <Link href="/" className="flex items-center select-none whitespace-nowrap">
            <span className="font-orbitron text-base font-bold tracking-tight" style={{ color: "var(--ink)" }}>
              LLM
            </span><span className="font-outfit text-base font-semibold" style={{ color: "var(--accent)" }}>
              Rates
            </span>
          </Link>

          {/* Money rain — center of navbar, plays once on first page load */}
          {!animDone && (
            <span style={{
              position: "absolute",
              top: "50%",
              left: "50%",
              transform: "translate(-50%, -50%)",
              width: 160,
              height: 160,
              pointerEvents: "none",
              zIndex: 10,
            }}>
              <Lottie
                lottieRef={lottieRef}
                animationData={greenMoneyAnim}
                loop={false}
                autoplay
                onComplete={() => setAnimDone(true)}
                style={{ width: "100%", height: "100%" }}
              />
            </span>
          )}

          {/* Navigation links — centered */}
          <nav className="hidden md:flex items-center justify-center gap-1" aria-label="Main navigation">
            {NAV_LINKS.map(({ href, label }) => {
              const isActive = pathname === href || pathname.startsWith(href + "/")
              return (
                <Link
                  key={href}
                  href={href}
                  className={cn("relative px-3 py-1.5 text-sm font-medium font-outfit transition-colors")}
                  style={{
                    color: isActive ? "var(--ink)" : "var(--muted)",
                    fontWeight: isActive ? 600 : 500,
                  }}
                  aria-current={isActive ? "page" : undefined}
                >
                  {label}
                </Link>
              )
            })}
          </nav>

          {/* Right-side CTA */}
          {!pathname.startsWith("/signup") && (
            <div className="hidden md:flex items-center justify-end">
              <Link
                href="/signup/free"
                className="font-outfit text-sm font-semibold px-4 py-1.5"
                style={{
                  backgroundColor: "var(--accent)",
                  color: "var(--surfaceHi)",
                  border: "1px solid var(--accentDk)",
                }}
              >
                Get API Key
              </Link>
            </div>
          )}

          {/* Mobile menu button */}
          <button
            type="button"
            className="justify-self-end p-2 md:hidden"
            style={{ color: "var(--muted)", border: "1px solid var(--border)" }}
            aria-label={mobileOpen ? "Close navigation menu" : "Open navigation menu"}
            aria-expanded={mobileOpen}
            aria-controls="mobile-nav"
            onClick={() => setMobileOpen((o) => !o)}
          >
            <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
              <path d="M2 4h14M2 9h14M2 14h14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" />
            </svg>
          </button>
        </div>

        {/* Mobile nav */}
        <nav
          id="mobile-nav"
          className="md:hidden border-t"
          style={{ borderColor: "var(--border)", backgroundColor: "var(--bg)" }}
          aria-label="Mobile navigation"
          hidden={!mobileOpen}
        >
          {NAV_LINKS.map(({ href, label }) => {
            const isActive = pathname === href || pathname.startsWith(href + "/")
            return (
              <Link
                key={href}
                href={href}
                className="block px-6 py-3 text-sm font-medium font-outfit"
                style={{
                  color: isActive ? "var(--accent)" : "var(--text)",
                  borderLeft: isActive ? "3px solid var(--accent)" : "3px solid transparent",
                  backgroundColor: isActive ? "var(--accentLt)" : "transparent",
                }}
                aria-current={isActive ? "page" : undefined}
                onClick={() => setMobileOpen(false)}
              >
                {label}
              </Link>
            )
          })}
        </nav>
      </div>
    </header>
  )
}
