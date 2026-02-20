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
        darkMode: false,
      }}
    />
  )
}
