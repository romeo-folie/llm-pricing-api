import type { NextConfig } from "next"

// CSP NOTE: 'unsafe-inline' in script-src is required for Next.js SSR hydration
// (Next.js injects inline <script> blocks for initial state). Removing it requires
// nonce-based CSP via Next.js middleware (see:
// https://nextjs.org/docs/app/building-your-application/configuring/content-security-policy#nonce).
// This is tracked as a follow-up hardening task. The CDN risk is mitigated by
// removing all third-party script-src entries; @google/model-viewer is self-hosted
// via the Next.js bundler at /_next/static/chunks/*.
const ContentSecurityPolicy = `
  default-src 'self';
  script-src 'self' 'unsafe-inline';
  style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;
  font-src 'self' https://fonts.gstatic.com;
  img-src 'self' data: blob: https:;
  media-src 'self' blob:;
  connect-src 'self' https://checkout.lemonsqueezy.com https://assets10.lottiefiles.com;
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

const nextConfig: NextConfig = {
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: securityHeaders,
      },
    ]
  },
  images: {
    remotePatterns: [],
  },
}

export default nextConfig
