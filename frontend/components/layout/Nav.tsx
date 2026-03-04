"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useState } from "react"
import { cn } from "@/lib/utils"

const NAV_LINKS = [
  { href: "/models",     label: "Models"     },
  { href: "/compare",    label: "Compare"    },
  { href: "/calculator", label: "Calculator" },
  { href: "/changes",    label: "Changes"    },
  { href: "/docs",       label: "Docs"       },
  { href: "/pricing",    label: "Features"   },
] as const

export default function Nav() {
  const pathname = usePathname()
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <header
      className="sticky top-0 z-50 w-full"
      style={{ backgroundColor: "var(--bg)", borderBottom: "1px solid var(--border)" }}
    >
      <div
        className="mx-auto flex h-16 max-w-7xl items-center justify-between"
      >
        {/* Logo */}
        <Link href="/" className="flex items-center gap-0.5 select-none">
          <span
            className="font-orbitron text-base font-bold"
            style={{ color: "var(--ink)" }}
          >
            LLM
          </span>
          <span
            className="font-outfit text-base font-semibold"
            style={{ color: "var(--accent)" }}
          >
            Rates
          </span>
        </Link>

        {/* Navigation links — center */}
        <nav className="hidden md:flex items-center gap-1" aria-label="Main navigation">
          {NAV_LINKS.map(({ href, label }) => {
            const isActive = pathname === href || pathname.startsWith(href + "/")
            return (
              <Link
                key={href}
                href={href}
                className={cn(
                  "relative px-3 py-1.5 text-sm font-medium transition-colors",
                  "font-outfit",
                )}
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

        {/* Right-side CTAs */}
        <div className="hidden md:flex items-center gap-2">
          <a
            href={process.env.NEXT_PUBLIC_LS_CHECKOUT_DEV || "/pricing"}
            className="font-outfit text-sm font-semibold px-4 py-1.5"
            style={{
              backgroundColor: "var(--accent)",
              color: "var(--surfaceHi)",
              border: "1px solid var(--accentDk)",
            }}
          >
            Get API Key
          </a>
        </div>

        {/* Mobile menu button */}
        <button
          type="button"
          className="md:hidden p-2"
          style={{ color: "var(--muted)", border: "1px solid var(--border)" }}
          aria-label={mobileOpen ? "Close navigation menu" : "Open navigation menu"}
          aria-expanded={mobileOpen}
          aria-controls="mobile-nav"
          onClick={() => setMobileOpen((o) => !o)}
        >
          <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
            <path d="M2 4h14M2 9h14M2 14h14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square"/>
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
    </header>
  )
}
