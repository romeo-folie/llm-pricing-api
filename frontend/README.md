# frontend/

Next.js 16 (App Router + SSR) public-facing web interface for the LLM Token Pricing Platform.

## Purpose

Surfaces reconciled LLM token pricing data from the Phase 2 REST API in a distinctive editorial aesthetic. All pages are fully public (no auth). The frontend is the primary acquisition and conversion surface.

## Stack

| Layer | Choice |
|---|---|
| Framework | Next.js 16 (App Router, SSR) |
| Language | TypeScript (strict mode) |
| Styling | Tailwind CSS v4 + design tokens |
| Components | shadcn/ui (stone base) |
| Charts | Recharts (tasks #23, #26) |
| Fonts | Geist Sans + Geist Mono via `geist` package |
| Hero | Inline SVG architecture diagram (server component, zero client JS) |
| Deployment | Railway (Nixpacks) |

## Directory Structure

```
frontend/
├── app/
│   ├── layout.tsx          # Root layout: fonts, Nav, Footer, metadata
│   ├── page.tsx            # Landing page with hero diagram + SSR stats
│   ├── globals.css         # Tailwind v4 @theme block + all design tokens
│   └── api/                # Next.js route handlers (proxy routes for client polling)
├── components/
│   ├── layout/
│   │   ├── Nav.tsx         # Sticky top navigation ("use client" for active state)
│   │   └── Footer.tsx      # Footer with product, dev, and agent discovery links
│   ├── ui/                 # shadcn/ui primitives (Button, Dialog, Badge, etc.)
│   ├── model/              # Model browser, detail modal, price history chart (task #23)
│   └── hero/               # HeroScene SVG architecture diagram
├── lib/
│   ├── api.ts              # Server-only typed API client (all REST endpoints)
│   └── utils.ts            # cn() class merging utility
├── public/                 # Static assets (SVGs, favicon)
├── .env.example            # Required environment variables
├── components.json         # shadcn/ui configuration
├── next.config.ts          # Security headers (CSP, X-Frame-Options, etc.)
├── railway.json            # Railway deployment config (Nixpacks)
└── tsconfig.json           # TypeScript strict mode
```

## Design System

The design language is locked in `../.claude/frontend-design-spec.md`. Key rules:

- **Palette**: warm bone/ivory base (`--bg: #F2EDE8`), deep teal accent (`--accent: #107E72`)
- **No box-shadows** — depth via `border border-[--border]` only
- **No colored top borders on cards** — uniform `1px solid var(--border)` on all sides
- **Typography**: `font-orbitron` (Geist Mono) for all numbers/prices, `font-outfit` (Geist Sans) for all other text
- **Design tokens**: defined in `app/globals.css` `@theme` block — accessible as Tailwind utilities (`bg-accent`, `text-muted`, `font-orbitron`) and as raw CSS vars (`var(--accent)`)

## Environment Variables

Copy `.env.example` to `.env.local` and fill in values:

| Variable | Side | Purpose |
|---|---|---|
| `LLM_PRICING_API_BASE_URL` | Server | REST API base URL |
| `LLM_PRICING_API_KEY` | Server | Dev-tier API key for history/recommend endpoints |
| `NEXT_PUBLIC_LS_CHECKOUT_DEV` | Client | Lemon Squeezy checkout URL (Developer plan) |
| `NEXT_PUBLIC_LS_CHECKOUT_PRO` | Client | Lemon Squeezy checkout URL (Pro plan) |
| `NEXT_PUBLIC_SITE_URL` | Client | Canonical site URL for OG tags / sitemap |

## API Client (`lib/api.ts`)

Server-only module (`import 'server-only'` guard prevents accidental client-side imports). All functions correspond to REST API endpoints:

| Function | Endpoint | Tier |
|---|---|---|
| `getModels(filter?)` | `GET /v1/models` | Free |
| `getModel(id)` | `GET /v1/models/:id` | Free |
| `getModelHistory(id, from?, to?)` | `GET /v1/models/:id/history` | Dev+ |
| `getProviders()` | `GET /v1/providers` | Free |
| `getCompare(models[])` | `GET /v1/compare` | Free |
| `getChanges(filter?)` | `GET /v1/changes` | Free |

All fetches use `next: { revalidate: 300 }` (5-minute ISR). History uses 60s revalidation.

## Local Development

```bash
# Install dependencies
npm install

# Set up environment
cp .env.example .env.local
# Edit .env.local with your API URL and key

# Start dev server (http://localhost:3000)
npm run dev

# Type check
npx tsc --noEmit

# Production build + start
npm run build
npm start
```

## Deployment (Railway)

`railway.json` configures a Nixpacks build:

```json
{
  "build": { "builder": "NIXPACKS", "buildCommand": "npm run build" },
  "deploy": { "startCommand": "npm start", "healthcheckPath": "/" }
}
```

Set all env vars in the Railway service dashboard. `LLM_PRICING_API_KEY` must be a server-side secret — never use a `NEXT_PUBLIC_` prefix for it.

## Security

- `LLM_PRICING_API_KEY` is server-only — enforced by `import 'server-only'` in `lib/api.ts`
- CSP, `X-Content-Type-Options`, and `X-Frame-Options` set in `next.config.ts`; CSP allows Lemon Squeezy checkout
- All user inputs (calculator token counts, filter params) validated server-side before any price calculation
- No dark mode — single fixed light theme per design spec
