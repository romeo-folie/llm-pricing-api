import "@/app/signup/signup.css"
import type { ReactNode } from "react"

// Reuses signup.css so the inline sign-in form on this page has the same form
// controls as the rest of the signup flow. New page-specific styles live in
// activate.css, imported by ActivateFlow.
export default function ActivateLayout({ children }: { children: ReactNode }) {
  return children
}
