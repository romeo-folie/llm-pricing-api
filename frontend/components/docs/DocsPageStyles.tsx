"use client"

import { useEffect } from "react"

/**
 * Adds `data-page="docs"` to the <html> element while the docs page is mounted.
 * This allows CSS in globals.css to target the nav specifically on the docs page
 * without affecting any other page.
 */
export default function DocsPageStyles() {
  useEffect(() => {
    document.documentElement.setAttribute("data-page", "docs")
    return () => {
      document.documentElement.removeAttribute("data-page")
    }
  }, [])

  return null
}
