"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useState, useRef } from "react"
import Lottie, { LottieRefCurrentProps } from "lottie-react"
import moneyStackAnim from "@/public/animations/money-stack.json"
import moneyStackCoinsAnim from "@/public/animations/money-stack-w-coins.json"
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

  return (
    <header
      className="sticky top-0 z-50 w-full"
      style={{ backgroundColor: "var(--bg)" }}
    >
      <div className="mx-auto max-w-[1280px] relative px-0" style={{ backgroundColor: "var(--bg)" }}>
        {/* Animated Custom Borders (Start Sequence) */}
        {/* Left Nav Rail */}
        <div className="absolute top-0 left-0 w-px h-full" style={{ backgroundColor: "var(--border)", transformOrigin: "top", transform: "scaleY(0)", animation: "draw-vertical-rail 0.3s cubic-bezier(0.77, 0, 0.175, 1) forwards", animationDelay: "0.0s" }} />
        {/* Right Nav Rail */}
        <div className="absolute top-0 right-0 w-px h-full" style={{ backgroundColor: "var(--border)", transformOrigin: "top", transform: "scaleY(0)", animation: "draw-vertical-rail 0.3s cubic-bezier(0.77, 0, 0.175, 1) forwards", animationDelay: "0.0s" }} />
        {/* Bottom Nav Rail (Left to Center) */}
        <div className="absolute bottom-0 left-0 h-px w-1/2" style={{ backgroundColor: "var(--border)", transformOrigin: "left", transform: "scaleX(0)", animation: "draw-horizontal-rail 0.3s cubic-bezier(0.77, 0, 0.175, 1) forwards", animationDelay: "0.3s" }} />
        {/* Bottom Nav Rail (Right to Center) */}
        <div className="absolute bottom-0 right-0 h-px w-1/2" style={{ backgroundColor: "var(--border)", transformOrigin: "right", transform: "scaleX(0)", animation: "draw-horizontal-rail 0.3s cubic-bezier(0.77, 0, 0.175, 1) forwards", animationDelay: "0.3s" }} />

        <div className="grid h-13 grid-cols-[auto_1fr_auto] items-center pl-2 pr-2 md:h-14 md:pl-4 md:pr-4" style={{ position: "relative", zIndex: 10 }}>

          {/* Animation + Logo */}
          <div className="flex items-center gap-1">
            <div style={{ marginLeft: "-8px", marginRight: "-10px", transform: "translateY(2px)" }}>
              <Lottie
                animationData={moneyStackCoinsAnim}
                loop={false}
                autoplay={true}
                style={{ width: "48px", height: "48px", transform: "scaleX(-1)" }}
              />
            </div>
            <Link
              href="/"
              className="flex items-center select-none whitespace-nowrap"
            >
              <span className="font-orbitron text-base font-bold tracking-tight" style={{ color: "var(--ink)" }}>
                {"LLM".split("").map((c, i) => (
                  <span key={`l-${i}`} className="animate-reveal-char" style={{ animationDelay: `${1.3 + i * 0.08}s` }}>
                    {c}
                  </span>
                ))}
              </span><span className="font-outfit text-base font-semibold" style={{ color: "var(--accent)" }}>
                {"Rates".split("").map((c, i) => (
                  <span key={`r-${i}`} className="animate-reveal-char" style={{ animationDelay: `${1.54 + i * 0.08}s` }}>
                    {c}
                  </span>
                ))}
              </span>
            </Link>
          </div>


          {/* Navigation links — centered */}
          <nav className="hidden md:flex items-center justify-center gap-1" aria-label="Main navigation">
            {NAV_LINKS.map(({ href, label }, i) => {
              const isActive = pathname === href || pathname.startsWith(href + "/")
              return (
                <Link
                  key={href}
                  href={href}
                  className={cn("relative px-3 py-1.5 text-sm font-medium font-outfit transition-colors animate-reveal-nav-link")}
                  style={{
                    color: isActive ? "var(--ink)" : "var(--muted)",
                    fontWeight: isActive ? 600 : 500,
                    animationDelay: `${1.7 + i * 0.05}s`
                  }}
                  aria-current={isActive ? "page" : undefined}
                >
                  {label}
                </Link>
              )
            })}
          </nav>

          {/* Right-side CTA */}
          <div className="flex items-center justify-end gap-2">
            {!pathname.startsWith("/signup") && (
              <Link
                href="/signup/free"
                className="hidden sm:flex font-outfit text-sm font-semibold px-4 py-1.5 animate-reveal-cta"
                style={{
                  backgroundColor: "var(--accent)",
                  color: "var(--surfaceHi)",
                  border: "1px solid var(--accentDk)",
                  animationDelay: "2.1s"
                }}
              >
                Get API Key
              </Link>
            )}

            {/* Mobile menu button */}
            <button
              type="button"
              className="p-2 md:hidden"
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
          
          {/* Mobile CTA */}
          {!pathname.startsWith("/signup") && (
            <div className="px-6 py-4 border-t" style={{ borderColor: "var(--border)" }}>
              <Link
                href="/signup/free"
                className="block w-full text-center font-outfit text-sm font-semibold px-4 py-3"
                style={{
                  backgroundColor: "var(--accent)",
                  color: "var(--surfaceHi)",
                  border: "1px solid var(--accentDk)",
                }}
                onClick={() => setMobileOpen(false)}
              >
                Get Free API Key
              </Link>
            </div>
          )}
        </nav>
      </div>
    </header>
  )
}
