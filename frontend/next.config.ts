import type { NextConfig } from "next"

// CSP NOTE: 'unsafe-inline' in script-src is required for Next.js SSR hydration
// (Next.js injects inline <script> blocks for initial state). Removing it requires
// nonce-based CSP via Next.js middleware (see:
// https://nextjs.org/docs/app/building-your-application/configuring/content-security-policy#nonce).
// This is tracked as a follow-up hardening task.
const ContentSecurityPolicy = `
  default-src 'self';
  script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' https://www.googletagmanager.com;
  style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net;
  font-src 'self' data: https://fonts.scalar.com;
  img-src 'self' data: blob: https:;
  media-src 'self';
  connect-src 'self' https://api.llmrates.live https://cdn.jsdelivr.net https://checkout.lemonsqueezy.com https://www.google-analytics.com https://analytics.google.com https://*.google-analytics.com https://*.analytics.google.com;
  worker-src 'self' blob:;
  frame-src https://checkout.lemonsqueezy.com;
  object-src 'none';
  base-uri 'self';
  form-action 'self' https://checkout.lemonsqueezy.com;
  upgrade-insecure-requests;
`.replace(/\s{2,}/g, " ").trim()

const securityHeaders = [
  {
    key: "Content-Security-Policy",
    value: ContentSecurityPolicy,
  },
  {
    key: "X-Content-Type-Options",
    value: "nosniff",
  },
  {
    key: "X-Frame-Options",
    value: "DENY",
  },
  {
    key: "X-DNS-Prefetch-Control",
    value: "on",
  },
  {
    key: "Referrer-Policy",
    value: "strict-origin-when-cross-origin",
  },
  {
    key: "Permissions-Policy",
    value: "camera=(), microphone=(), geolocation=()",
  },
]

const API_BASE = process.env.LLM_PRICING_API_BASE_URL || "http://localhost:8080"

const nextConfig: NextConfig = {
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: securityHeaders,
      },
    ]
  },
  async redirects() {
    return [
      { source: "/best-for", destination: "/compare", permanent: true },
      { source: "/best-for/:slug", destination: "/compare?use-case=:slug", permanent: true },
    ]
  },
  async rewrites() {
    return [
      { source: "/openapi.json",               destination: `${API_BASE}/openapi.json`               },
      { source: "/llms.txt",                   destination: `${API_BASE}/llms.txt`                   },
      { source: "/.well-known/ai-plugin.json", destination: `${API_BASE}/.well-known/ai-plugin.json` },
      // Signup flow — proxy all /auth/signup/* calls to the Go backend.
      // The backend handles session cookies and issues the redirect to
      // /signup/free?verified=1 after magic-link verification.
      { source: "/auth/signup/:path*",          destination: `${API_BASE}/auth/signup/:path*`          },
    ]
  },
  images: {
    remotePatterns: [],
  },
}

export default nextConfig
