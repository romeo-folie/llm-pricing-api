"use client"

// Renders the current year client-side so it is never frozen at build time.
export default function CopyrightYear() {
  return <>{new Date().getFullYear()}</>
}
