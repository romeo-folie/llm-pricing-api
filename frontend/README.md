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
| Charts | Recharts |
| Fonts | Geist Sans + Geist Mono via `geist` package |
| Hero | Inline SVG architecture diagram (server component, zero client JS) |
| Deployment | Vercel |

## Directory Structure

```
frontend/
├── app/
│   ├── layout.tsx              # Root layout: fonts, Nav, Footer, metadata template
│   ├── page.tsx                # Landing page with hero diagram + SSR stats
│   ├── globals.css             # Tailwind v4 @theme block + all design tokens
│   ├── sitemap.ts              # Dynamic sitemap (static + model routes)
│   ├── robots.ts               # Robots.txt generation
│   ├── opengraph-image.tsx     # Auto-generated OG image (1200x630)
│   ├── error.tsx               # Global error boundary
│   ├── not-found.tsx           # 404 page
│   ├── models/
│   │   ├── page.tsx            # SSR model browser with filters
│   │   ├── error.tsx           # Model browser error boundary
│   │   └── [id]/
│   │       ├── page.tsx        # Model detail with price history chart
│   │       └── error.tsx       # Model detail error boundary
│   ├── compare/page.tsx        # Side-by-side model comparison
│   ├── calculator/
│   │   ├── page.tsx            # Cost calculator page
│   │   └── actions.ts          # Server action for cost calculation
│   ├── changes/page.tsx        # Real-time price change feed (60s polling)
│   ├── pricing/page.tsx        # Static pricing page with tier cards + FAQ
│   └── api/                    # Route handlers (proxy routes for client polling)
│       ├── health/route.ts     # Health check endpoint
│       ├── changes/route.ts    # Changes proxy for client-side polling
│       └── model/[id]/route.ts # Model detail proxy
├── components/
│   ├── analytics/
│   │   └── GoogleAnalytics.tsx # GA4 script loader + route change tracker
│   ├── layout/
│   │   ├── Nav.tsx             # Sticky top navigation (client component)
│   │   └── Footer.tsx          # Footer with product, dev, and agent links
│   ├── ui/                     # shadcn/ui primitives (Button, Dialog, Badge, etc.)
│   ├── hero/
│   │   ├── HeroScene.tsx       # SVG isometric architecture diagram
│   │   └── index.ts            # Re-export
│   ├── model/
│   │   ├── ModelCard.tsx       # Individual model row in browser
│   │   ├── ModelDetailModal.tsx # Modal triggered by ?model= param
│   │   ├── ModelPicker.tsx     # Model selector (compare + calculator)
│   │   └── PriceHistoryChart.tsx # Recharts line chart (7d/30d/90d/All)
│   ├── compare/
│   │   ├── CompareClient.tsx   # Compare page client logic
│   │   ├── CompareTable.tsx    # Side-by-side comparison table
│   │   └── ModelPicker.tsx     # Model picker for compare
│   ├── changes/
│   │   ├── ChangesFeed.tsx     # Change feed with polling + filters
│   │   └── ChangeRow.tsx       # Individual change row
│   └── calculator/
│       └── CalculatorClient.tsx # Calculator with daily/monthly/yearly toggle
├── lib/
│   ├── analytics.ts            # GA4 pageview + typed custom event helpers
│   ├── api.ts                  # Server-only typed API client (all REST endpoints)
│   ├── cache-codec.ts          # gzip+base64 encode/decode for large data-cache entries
│   ├── models-filter.ts        # /models search-param parsing, in-memory search, pagination
│   └── utils.ts                # cn() class merging utility
├── public/                     # Static assets (SVGs, favicon)
├── .env.example                # Required environment variables
├── components.json             # shadcn/ui configuration
├── next.config.ts              # Security headers (CSP, X-Frame-Options, etc.)
├── vercel.json                 # Vercel deployment config (optional — only needed for custom headers beyond next.config.ts)
└── tsconfig.json               # TypeScript strict mode
```

## Design System

The design language is locked in `../.claude/frontend-design-spec.md`. Key rules:

- **Palette**: warm bone/ivory base (`--bg: #F2EDE8`), teal accent (`--accent: #107E72`)
- **No box-shadows** — depth via `border border-[--border]` only
- **No colored top borders on cards** — uniform `1px solid var(--border)` on all sides
- **Typography**: `font-orbitron` (Geist Mono) for all numbers/prices, `font-outfit` (Geist Sans) for all other text
- **Design tokens**: defined in `app/globals.css` `@theme` block — accessible as Tailwind utilities and as raw CSS vars (`var(--accent)`)

## Environment Variables

Copy `.env.example` to `.env.local` and fill in values:

| Variable | Side | Purpose |
|---|---|---|
| `LLM_PRICING_API_BASE_URL` | Server | REST API base URL (default: `http://localhost:8080`) |
| `LLM_PRICING_API_KEY` | Server | API key used for all server-side calls to the REST API |
| `NEXT_PUBLIC_SITE_URL` | Client | Canonical site URL for OG tags / sitemap (default: `https://llmrates.live`) |
| `NEXT_PUBLIC_GA_MEASUREMENT_ID` | Client | GA4 measurement ID (e.g. `G-XXXXXXXXXX`). Leave blank to disable analytics. |

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

### Caching and Vercel ISR writes

Vercel bills every Next.js data-cache write (`fetch` with `next.revalidate`, and `unstable_cache`) as an **ISR write**, even on `force-dynamic` pages. What counts toward the quota is the number of **distinct cache keys** times how often each is refreshed, so keep keys bounded:

- Most fetches use `next: { revalidate: 300 }`. `getModelHistory` uses 900s; prices change only a few times a day.
- `getModels(filter)` caches the **whole catalog in one gzipped `unstable_cache` entry** (`models:all:v2`, 900s). Provider, modality, context, sort and search filters are applied in memory and never enter the cache key. Inner page fetches use `no-store` and stable `sort=alpha` ordering. Each pass de-duplicates IDs, tolerates a shortfall of at most 0.5% with a warning, and retries larger shortfalls once before throwing. Decoded catalog memoization is bounded to two sort orders. Bump the key version when the cached model shape changes.
- Paginated changes always use `no-store`: rolling timestamps and cursors have little reuse. Changes and summaries with custom date/provider filters also bypass the Vercel data cache. The Go API still applies its Redis response cache. Explicit cache modes suppress the default `next.revalidate` to avoid conflicting Next.js options.
- Free-text search (`q`) is **never sent upstream**. `getModels` applies it in memory (`filterModelsByQuery`: case-insensitive literal substring on name or slug), so search strings never become cache keys. `getModelsPaginated` has no `q` parameter for the same reason.
- `/models` parses its search params with `parseModelsSearchParams`, which accepts only values the UI can produce: known modalities and the `min_context` dropdown values. It resolves the provider against `getProviders()` with `resolveProvider`, so unknown or wildcard values (`%`, `_`) can't create cache entries, then paginates in memory with `paginate`. `/providers/[slug]` resolves its slug the same way and returns 404 before touching the model cache.
- `/api/model/[id]` ignores caller-supplied `from`/`to`, and returns a fixed error message while logging the upstream detail server-side.
- `getModel(id)` rejects numeric aliases (`042`, `+42`) without fetching. The backend's `strconv.Atoi` would resolve each of them to the same model under a different cached URL.
- Walking the pages relies on the backend's `ORDER BY` ending in the unique `m.id` (see `internal/api/handlers/README.md`). Without it, rows tied at page boundaries would make every pass come back incomplete.

Don't pass user-controlled, high-cardinality values (search text, timestamps) into a cached fetch URL. Filter them after the cached call, or fetch them with `cache: "no-store"`.

## SEO

- **Metadata**: Root layout defines `title.template: "%s — LLMRates"`. Child pages export just the page-specific title.
- **Open Graph**: Per-page `og:title` and `og:description`. Root layout includes `twitter` card metadata.
- **OG Image**: Auto-generated via `app/opengraph-image.tsx` (1200x630 PNG).
- **JSON-LD**: `Dataset` schema on `/models`, `Product + Offer` schema on `/models/[id]`.
- **Sitemap**: `app/sitemap.ts` generates static routes + dynamic `/models/[id]` routes (capped at 1000).
- **Robots**: `app/robots.ts` allows all crawlers, disallows `/api/`, references `/sitemap.xml`.

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

## Analytics (GA4)

Google Analytics 4 integration is built in. Set `NEXT_PUBLIC_GA_MEASUREMENT_ID` to enable it.

**What's tracked automatically:**
- Pageviews on every client-side navigation (App Router route changes)
- GA4 enhanced measurement (outbound clicks, scroll depth, site search — enabled in GA4 dashboard)

**Custom events (fired from components):**

| Event | Component | Trigger |
|---|---|---|
| `compare_models` | `CompareClient` | User selects 2+ models for comparison |
| `calculate_cost` | `CalculatorClient` | Cost calculation completes |
| `view_model_detail` | `ModelDetailModal` | Model detail modal opens |
| `add_to_compare` | `ModelDetailModal` | "Add to Compare" clicked |
| `share_comparison` | `CompareClient` | Share URL copied |
| `click_pricing_cta` | (available) | Helper exported in `lib/analytics.ts` |

To add tracking to a new component, import from `lib/analytics.ts`:
```ts
import { trackCompareModels } from "@/lib/analytics"
trackCompareModels(["gpt-4o", "claude-sonnet-4"])
```

## Deployment (Vercel)

Vercel auto-detects Next.js and requires no build configuration. Import the `frontend/` directory as the Vercel project root (set **Root Directory** to `frontend` in the project settings).

**Environment variables** — set these in the Vercel dashboard under _Settings → Environment Variables_:

| Variable | Environments | Notes |
|---|---|---|
| `LLM_PRICING_API_BASE_URL` | Production, Preview | REST API base URL |
| `LLM_PRICING_API_KEY` | Production, Preview | Server-only — never expose to client |
| `NEXT_PUBLIC_SITE_URL` | Production | Canonical URL (e.g. `https://llmrates.live`) |
| `NEXT_PUBLIC_GA_MEASUREMENT_ID` | Production | GA4 measurement ID (`G-XXXXXXXXXX`) |

`LLM_PRICING_API_KEY` must never be prefixed with `NEXT_PUBLIC_` — it is enforced as server-only by `import 'server-only'` in `lib/api.ts`.

Vercel runs `npm run build` automatically on each push and serves the output via its global edge network. No `startCommand`, Nixpacks builder, or `railway.json` is needed.

## Security

- `LLM_PRICING_API_KEY` is server-only — enforced by `import 'server-only'` in `lib/api.ts`
- CSP, `X-Content-Type-Options`, and `X-Frame-Options` set in `next.config.ts`
- All user inputs (calculator token counts, filter params) validated server-side before any price calculation
- No dark mode — single fixed light theme per design spec
