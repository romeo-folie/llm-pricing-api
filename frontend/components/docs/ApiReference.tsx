"use client"

import { ApiReferenceReact } from "@scalar/api-reference-react"
import "@scalar/api-reference-react/style.css"
import { useEffect, useState } from "react"

// Override Scalar's default theme variables to match the site's warm palette
// and teal accent. Scalar exposes CSS custom properties on `.scalar-app` and
// the `.light-mode` / `.dark-mode` scope classes.
const customCss = `
  .light-mode,
  .scalar-app:not(.dark-mode) {
    --scalar-color-1:        #1C1917;
    --scalar-color-2:        #78716C;
    --scalar-color-3:        #A8A29E;
    --scalar-background-1:   #F2EDE8;
    --scalar-background-2:   #F2EDE8;
    --scalar-background-3:   #F2EDE8;
    --scalar-border-color:   #DDD7D0;
    --scalar-color-accent:   #107E72;
    --scalar-button-1:       #107E72;
    --scalar-button-1-color: #FFFFFF;
    --scalar-color-green:    #059669;
    --scalar-color-blue:     #2563EB;
    --scalar-color-orange:   #D97706;
    --scalar-color-red:      #DC2626;
    --scalar-color-purple:   #7C3AED;
  }

  .dark-mode {
    --scalar-color-1:        #E8E4E0;
    --scalar-color-2:        #9C9491;
    --scalar-color-3:        #6B6461;
    --scalar-background-1:   #2B2A28;
    --scalar-background-2:   #2B2A28;
    --scalar-background-3:   #2B2A28;
    --scalar-background-4:   #2B2A28;
    --scalar-border-color:   #413F3C;
    --scalar-color-accent:   #13A092;
    --scalar-button-1:       #13A092;
    --scalar-button-1-color: #FFFFFF;
    --scalar-color-green:    #10B981;
    --scalar-color-blue:     #60A5FA;
    --scalar-color-orange:   #FBBF24;
    --scalar-color-red:      #F87171;
    --scalar-color-purple:   #A78BFA;
  }

  /* Force dark palette when system preference is dark, even if Scalar
     renders with .light-mode class (its detection can miss the preference). */
  @media (prefers-color-scheme: dark) {
    .light-mode,
    .scalar-app {
      --scalar-color-1:        #E8E4E0 !important;
      --scalar-color-2:        #9C9491 !important;
      --scalar-color-3:        #6B6461 !important;
      --scalar-background-1:   #2B2A28 !important;
      --scalar-background-2:   #2B2A28 !important;
      --scalar-background-3:   #2B2A28 !important;
      --scalar-background-4:   #2B2A28 !important;
      --scalar-border-color:   #413F3C !important;
      --scalar-color-accent:   #13A092 !important;
      --scalar-button-1:       #13A092 !important;
      --scalar-button-1-color: #FFFFFF !important;
      --scalar-color-green:    #10B981 !important;
      --scalar-color-blue:     #60A5FA !important;
      --scalar-color-orange:   #FBBF24 !important;
      --scalar-color-red:      #F87171 !important;
      --scalar-color-purple:   #A78BFA !important;
      color-scheme: dark !important;
    }
  }

  /* Theme is driven entirely by system preference (prefers-color-scheme media
     query). The component passes forceDarkModeState to Scalar so it applies
     the correct .dark-mode / .light-mode class; the toggle is hidden so users
     aren't presented with a conflicting manual override. */
  .scalar-app [data-cy="darkmode-toggle"],
  .scalar-app .darkmode-toggle,
  .scalar-app [aria-label*="dark mode"],
  .scalar-app [aria-label*="light mode"],
  .scalar-app [aria-label*="Dark mode"],
  .scalar-app [aria-label*="Light mode"] {
    display: none !important;
  }

  /* Fix sidebar border height and stickiness */
  .scalar-app aside {
    position: sticky !important;
    top: 56px !important; /* matches nav header height */
    height: calc(100vh - 56px) !important;
    min-height: calc(100vh - 56px) !important;
    max-height: calc(100vh - 56px) !important;
    align-self: flex-start !important;
    overflow-y: auto !important;
    border-right: 1px solid var(--scalar-border-color) !important;
  }
`

interface ApiReferenceProps {
  specUrl: string
}

export default function ApiReference({ specUrl }: ApiReferenceProps) {
  const [isDark, setIsDark] = useState(false)

  useEffect(() => {
    const mql = window.matchMedia("(prefers-color-scheme: dark)")
    setIsDark(mql.matches)
    const handler = (e: MediaQueryListEvent) => setIsDark(e.matches)
    mql.addEventListener("change", handler)
    return () => mql.removeEventListener("change", handler)
  }, [])

  return (
    <ApiReferenceReact
      key={isDark ? "dark" : "light"}
      configuration={{
        url: specUrl,
        theme: "default",
        customCss,
        darkMode: isDark,
        forceDarkModeState: isDark ? "dark" : "light",
        hideDarkModeToggle: true,
      }}
    />
  )
}
