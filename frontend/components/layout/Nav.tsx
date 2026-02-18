"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { cn } from "@/lib/utils"

const NAV_LINKS = [
  { href: "/models",     label: "Models"     },
  { href: "/compare",    label: "Compare"    },
  { href: "/calculator", label: "Calculator" },
  { href: "/changes",    label: "Changes"    },
  { href: "/pricing",    label: "Pricing"    },
] as const

export default function Nav() {
  const pathname = usePathname()

  return (
    <header
      className="sticky top-0 z-50 w-full"
      style={{ backgroundColor: "var(--surface)", borderBottom: "1px solid var(--border)" }}
    >
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        {/* Logo */}
        <Link href="/" className="flex items-center gap-1 select-none group">
          {/* Pulsing LIVE dot */}
          <span
            className="animate-live mr-2 inline-block h-2 w-2 rounded-full"
            style={{ backgroundColor: "var(--accent)" }}
            aria-hidden="true"
          />
          <span
            className="font-orbitron text-lg font-bold tracking-tight"
            style={{ color: "var(--ink)", letterSpacing: "-0.02em" }}
          >
            LLM
          </span>
          <span
            className="font-outfit text-lg font-semibold"
            style={{ color: "var(--accent)" }}
          >
            Price
          </span>
        </Link>

        {/* Navigation links */}
        <nav className="hidden md:flex items-center gap-1" aria-label="Main navigation">
          {NAV_LINKS.map(({ href, label }) => {
            const isActive = pathname === href || pathname.startsWith(href + "/")
            return (
              <Link
                key={href}
                href={href}
                className={cn(
                  "relative px-3 py-1.5 text-sm font-medium transition-colors rounded-sm",
                  "font-outfit",
                )}
                style={{
                  color: isActive ? "var(--accent)" : "var(--muted)",
                  borderBottom: isActive ? "2px solid var(--accent)" : "2px solid transparent",
                }}
                aria-current={isActive ? "page" : undefined}
              >
                {label}
              </Link>
            )
          })}
        </nav>

        {/* Mobile menu button — accessible; full JS toggle deferred to a future task */}
        <button
          type="button"
          className="md:hidden p-2 rounded-sm"
          style={{ color: "var(--muted)", border: "1px solid var(--border)" }}
          aria-label="Open navigation menu"
          aria-expanded="false"
          aria-controls="mobile-nav"
          disabled
        >
          <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
            <path d="M2 4h14M2 9h14M2 14h14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square"/>
          </svg>
        </button>
      </div>

      {/* Mobile nav — inert until hamburger toggle is implemented */}
      <nav
        id="mobile-nav"
        className="md:hidden border-t"
        style={{ borderColor: "var(--border)", backgroundColor: "var(--surface)" }}
        aria-label="Mobile navigation"
        hidden
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
            >
              {label}
            </Link>
          )
        })}
      </nav>
    </header>
  )
}
