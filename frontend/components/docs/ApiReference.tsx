"use client"

import { ApiReferenceReact } from "@scalar/api-reference-react"
import "@scalar/api-reference-react/style.css"

// Override Scalar's default theme variables to match the site's warm palette
// and teal accent. Scalar exposes CSS custom properties on `.scalar-app` and
// the `.light-mode` / `.dark-mode` scope classes.
const customCss = `
  .light-mode,
  .scalar-app {
    --scalar-color-1:        #1C1917;
    --scalar-color-2:        #78716C;
    --scalar-color-3:        #A8A29E;
    --scalar-background-1:   #F2EDE8;
    --scalar-background-2:   #FDFAF7;
    --scalar-background-3:   #EDE8E2;
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
    --scalar-background-2:   #343230;
    --scalar-background-3:   #3E3C3A;
    --scalar-background-4:   #514E4B;
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
`

interface ApiReferenceProps {
  specUrl: string
}

export default function ApiReference({ specUrl }: ApiReferenceProps) {
  return (
    <ApiReferenceReact
      configuration={{
        url: specUrl,
        theme: "default",
        customCss,
      }}
    />
  )
}
