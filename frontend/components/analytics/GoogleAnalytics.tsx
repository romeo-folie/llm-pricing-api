"use client"

import Script from "next/script"
import { usePathname, useSearchParams } from "next/navigation"
import { useEffect, Suspense } from "react"
import { GA_MEASUREMENT_ID, pageview } from "@/lib/analytics"

/**
 * Inner component that tracks route changes.
 *
 * Wrapped in <Suspense> because useSearchParams() opts the nearest boundary
 * into client-side rendering in Next.js App Router.
 */
function RouteChangeTracker() {
  const pathname = usePathname()
  const searchParams = useSearchParams()

  useEffect(() => {
    if (!GA_MEASUREMENT_ID) return
    const url = searchParams.toString()
      ? `${pathname}?${searchParams.toString()}`
      : pathname
    pageview(url)
  }, [pathname, searchParams])

  return null
}

/**
 * Loads the GA4 gtag.js snippet and tracks page views on every
 * client-side navigation. Renders nothing when the measurement ID is missing.
 *
 * Usage: place <GoogleAnalytics /> inside the root <body> in layout.tsx.
 */
export default function GoogleAnalytics() {
  if (!GA_MEASUREMENT_ID) return null

  return (
    <>
      <Script
        src={`https://www.googletagmanager.com/gtag/js?id=${GA_MEASUREMENT_ID}`}
        strategy="afterInteractive"
      />
      <Script id="ga4-init" strategy="afterInteractive">
        {`
          window.dataLayer = window.dataLayer || [];
          function gtag(){dataLayer.push(arguments);}
          gtag('js', new Date());
          gtag('config', '${GA_MEASUREMENT_ID}', {
            page_path: window.location.pathname,
          });
        `}
      </Script>
      <Suspense fallback={null}>
        <RouteChangeTracker />
      </Suspense>
    </>
  )
}
