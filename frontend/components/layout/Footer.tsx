import Link from "next/link"
import CopyrightYear from "./CopyrightYear"

const PRODUCT_LINKS = [
  { href: "/models", label: "Model Browser" },
  { href: "/compare", label: "Compare" },
  { href: "/calculator", label: "LLM Price Calculator" },
  { href: "/changes", label: "Price Changes" },
  { href: "/why", label: "Why LLMRates?" },
  { href: "/pricing", label: "Features" },
] as const

// openapi.json is a rewrite to the Go API — use `native: true` to render a
// plain <a> tag and prevent Next.js from attempting an RSC prefetch on it.
const API_LINKS: { href: string; label: string; external?: boolean; native?: boolean }[] = [
  { href: "/docs",         label: "API Docs"    },
  { href: "/pricing",      label: "Features"    },
  { href: "/openapi.json", label: "OpenAPI Spec", native: true },
]

const DISCOVERY_LINKS = [
  { href: "/openapi.json",               label: "/openapi.json"               },
  { href: "/llms.txt",                   label: "/llms.txt"                   },
  { href: "/.well-known/ai-plugin.json", label: "/.well-known/ai-plugin.json" },
] as const

export default function Footer() {
  return (
    <footer
      className="w-full mt-auto"
    >
      <div
        className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-10 animate-draw-border-t-dk"
        style={{ '--draw-delay': '1.0s' } as React.CSSProperties}
      >
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-2 lg:grid-cols-4 animate-wireframe-fade" style={{ animationDelay: '1.5s' }}>
          {/* Brand */}
          <div className="lg:col-span-1">
            <div className="mb-3 flex items-center">
              <span
                className="font-orbitron text-base font-bold"
                style={{ color: "var(--ink)" }}
              >
                LLM
              </span><span
                className="font-outfit text-base font-semibold"
                style={{ color: "var(--accent)" }}
              >
                Rates
              </span>
            </div>
            <p className="text-xs font-outfit leading-relaxed" style={{ color: "var(--muted)" }}>
              Reconciled, source-attributed LLM token pricing with full price history. Built for agents and developers.
            </p>
          </div>

          {/* Product */}
          <div>
            <h3
              className="font-orbitron text-xs tracking-widest mb-3"
              style={{ color: "var(--dim)" }}
            >
              [ PRODUCT ]
            </h3>
            <ul className="space-y-2">
              {PRODUCT_LINKS.map(({ href, label }) => (
                <li key={href}>
                  <Link href={href} className="footer-link text-sm font-outfit">
                    {label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          {/* API */}
          <div>
            <h3
              className="font-orbitron text-xs tracking-widest mb-3"
              style={{ color: "var(--dim)" }}
            >
              [ DEVELOPERS ]
            </h3>
            <ul className="space-y-2">
              {API_LINKS.map(({ href, label, external, native }) => (
                <li key={href}>
                  {native ? (
                    <a href={href} className="footer-link text-sm font-outfit">
                      {label}
                    </a>
                  ) : (
                    <Link
                      href={href}
                      className="footer-link text-sm font-outfit"
                      {...(external ? { target: "_blank", rel: "noopener noreferrer" } : {})}
                    >
                      {label}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </div>

          {/* Discovery endpoints */}
          <div>
            <h3
              className="font-orbitron text-xs tracking-widest mb-3"
              style={{ color: "var(--dim)" }}
            >
              [ DISCOVERY ]
            </h3>
            <ul className="space-y-2">
              {DISCOVERY_LINKS.map(({ href, label }) => (
                <li key={href}>
                  <a href={href} className="footer-discovery-link font-orbitron text-xs">
                    {label}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        </div>

        {/* Bottom bar */}
        <div
          className="mt-8 pt-6 flex flex-col sm:flex-row justify-between items-center gap-2 animate-draw-border-t"
          style={{ '--draw-delay': '1.1s' } as React.CSSProperties}
        >
          <div className="flex flex-col sm:flex-row justify-between items-center gap-2 w-full animate-wireframe-fade" style={{ animationDelay: '1.6s' }}>
            <p className="font-outfit text-xs" style={{ color: "var(--dim)" }}>
              &copy; <CopyrightYear /> LLMRates. All prices reconciled from multiple sources.
            </p>
            <p className="font-orbitron text-xs" style={{ color: "var(--dim)" }}>
              p99 &lt;200ms &middot; updated every 5m
            </p>
          </div>
        </div>
      </div>
    </footer>
  )
}
