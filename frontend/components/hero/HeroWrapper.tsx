"use client"

import dynamic from "next/dynamic"
import HeroScene from "./HeroScene"

// Dynamically import the 3D canvas — client-only, ssr: false is allowed here
// because this is a Client Component. The SVG HeroScene renders during SSR
// (and as the loading fallback) so the layout doesn't shift on first paint.
const HeroModel3D = dynamic(() => import("./HeroModel3D"), {
  ssr: false,
  loading: () => <HeroScene />,
})

export default function HeroWrapper() {
  return <HeroModel3D />
}
