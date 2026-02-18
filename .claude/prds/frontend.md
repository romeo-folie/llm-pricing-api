---
name: frontend
description: Next.js SSR frontend for the LLM pricing platform — public-facing marketing, model browser, cost calculator, comparison, and pricing pages with a gamified isometric design language.
status: backlog
created: 2026-02-18T21:21:29Z
---

# PRD: frontend

## Executive Summary

Phase 3 delivers the public-facing web frontend for the LLM Token Pricing Platform. It is a Next.js (SSR) application that surfaces the reconciled pricing data from the Phase 2 REST API in a distinctive, gamified, isometric interface inspired by Greptile.com. All pages are publicly accessible — no authenticated dashboard in this phase. The frontend is the primary acquisition surface: it turns raw API data into a developer tool that people share.

---

## Problem Statement

The Phase 2 REST API has all the data. Phase 3 makes it discoverable and usable by humans, not just machines. Without a frontend:

- Developers can't evaluate the platform's data quality before purchasing an API key.
- There is no SEO surface to attract organic traffic from searches like "GPT-4 token pricing" or "Claude vs GPT cost comparison".
- The trust story ("reconciled, source-attributed, price history") is invisible unless visualised.
- The monetisation funnel (Free → Dev → Pro) has no conversion page.

---

## User Stories

### Persona A — Curious Developer
Arrives from a Google search. Wants to quickly compare token costs across 3–4 models for a project they're pricing.

- **US-1**: As a curious developer, I can browse all models on `/models` filtered by provider and modality, so I can find relevant models quickly.
- **US-2**: As a curious developer, I can open a model detail view (modal or page) and see current pricing plus a price history chart, so I understand pricing trends before committing.
- **US-3**: As a curious developer, I can navigate to `/compare` and select up to 5 models side-by-side, so I can make a cost-informed decision for my use case.

### Persona B — Cost-Conscious Builder
Has a project in production. Wants to model API costs at different token volumes.

- **US-4**: As a cost-conscious builder, I can use the cost calculator on `/calculator`, enter my expected input/output token counts and pick a model, so I can see the projected monthly cost.
- **US-5**: As a cost-conscious builder, I can share the calculator result (via URL params or copy link), so I can send it to a teammate.

### Persona C — Evaluating Buyer
Discovered the platform. Wants to understand what they get at each tier.

- **US-6**: As an evaluating buyer, I can visit `/pricing` and see Free, Developer ($15), and Pro ($50) tier cards with feature lists, so I can decide whether to purchase.
- **US-7**: As an evaluating buyer, I can click "Get started" on a pricing card and be taken to the Lemon Squeezy checkout, so I can subscribe.

### Persona D — AI Agent / LLM Tool
An LLM toolchain consuming the site programmatically.

- **US-8**: As an AI agent, I can discover the platform's capabilities via `/llms.txt` and `/.well-known/ai-plugin.json` (served by the API), so I can integrate it into my toolchain.
- **US-9**: As an AI agent, the `/models` page has structured JSON-LD so I can extract pricing data without calling the API.

---

## Requirements

### Functional Requirements

#### Pages & Routes

| Route | Purpose | SSR? | Notes |
|---|---|---|---|
| `/` | Landing page | Yes | Hero scene, value props, social proof, CTA to `/models` and `/pricing` |
| `/models` | Model browser | Yes | Filterable list; opens model detail modal; fallback to `/models/[id]` page if modal content is insufficient |
| `/models/[id]` | Model detail page | Yes | Fallback full page if the model has rich metadata (history chart + sources + trust metadata) |
| `/compare` | Side-by-side comparison | Yes (initial) | Client-side model selection; server-prefetch first 5 models |
| `/calculator` | Cost calculator | Partial | Input form is client-side; model list server-fetched |
| `/pricing` | Tier cards | Yes | Static-ish; Lemon Squeezy checkout links |
| `/changes` | Price change feed | Yes (initial) | Live updates via polling or SSE; game-event notification style |

#### Landing Page (`/`)
- Hero section with Blender MCP isometric scene (GLB rendered to WebP; fallback: LottieFiles "Isometric AI Animation")
- Value proposition: "Reconciled, source-attributed LLM token pricing. Full price history. Built for agents."
- Three-stat strip: total models tracked, providers covered, last price change timestamp (fetched SSR)
- Short feature highlights: price history, trust metadata, MCP + SSE agent endpoints
- Testimonials / social proof placeholder (can be dummy data in Phase 3)
- Pricing tier summary cards linking to `/pricing`
- CTA: "Browse models" → `/models` and "View docs" → external docs URL

#### Model Browser (`/models`)
- List of all models, fetched SSR via `GET /v1/models`
- Filters: provider (`?provider=`), modality (`?modality=`), minimum context (`?min_context=`); filters update URL params and trigger client-side refetch
- Each row/card shows: model name, provider badge, input price per 1M tokens, output price per 1M tokens, context window, "LIVE" last-updated badge, confidence indicator (high/medium/low)
- Clicking a model opens a **detail modal** (see below); modal URL-addressable via query param (`?model=gpt-4o`)
- If modal content is insufficient (no history, no multi-source data), link to `/models/[id]` full page instead

#### Model Detail (Modal / `/models/[id]`)
- Price history chart (Tremor or Recharts; isometric 3D bar chart treatment)
- Trust metadata strip: `confirmed_at`, source(s), `confidence`, `age_hours`, `change_velocity`
- Provider badge, context window, modalities
- "Add to compare" button → populates `/compare`
- Price history date range filter (`?from=`, `?to=`)
- Sources section: lists which scrapers confirm the price

#### Comparison Page (`/compare`)
- Select up to 5 models (search/autocomplete from `/v1/models`)
- Side-by-side table: provider, input price, output price, context, confidence, last updated
- "Best value" highlight (lowest input + output combined)
- Share URL with selected models encoded in query params
- Isometric game-board row aesthetic per design spec

#### Cost Calculator (`/calculator`)
- Model picker (search from `/v1/models`)
- Inputs: expected input tokens/day, output tokens/day
- Output: cost per day, per month (30 days), per year
- Toggle: daily / monthly / yearly view
- "Add another model" to compare costs side-by-side (up to 3 models)
- Results shareable via URL params
- Styled as isometric HUD / control panel per design spec

#### Pricing Page (`/pricing`)
- Three tier cards: Free ($0), Developer ($15/mo), Pro ($50/mo)
- Feature lists per tier (aligned with API endpoint access matrix in CLAUDE.md)
- "Get started" CTA per card → Lemon Squeezy checkout URL (env variable: `NEXT_PUBLIC_LS_CHECKOUT_DEV`, `NEXT_PUBLIC_LS_CHECKOUT_PRO`)
- Tier progression visual language (achievement/rank aesthetic per design spec)
- FAQ accordion below cards

#### Changes Feed (`/changes`)
- Recent price changes from `GET /v1/changes`
- Each entry: model name, provider, old price → new price, delta %, source, timestamp
- Game-event notification style (flash animation on new item)
- Filter by provider; date filter
- Polling every 60s for new changes (or SSE if practical)

### Non-Functional Requirements

#### Performance
- Core Web Vitals targets: LCP < 2.5s, CLS < 0.1, INP < 200ms
- All SSR pages must respond in < 500ms (relies on API p99 < 200ms + CDN edge caching)
- Hero WebP < 300KB; hero GLB lazy-loaded
- Images served via `next/image` with proper sizing

#### SEO
- SSR on all data pages (no client-only fetching for indexable content)
- `<title>` and `<meta description>` per page (dynamic for model pages)
- JSON-LD `Dataset` schema on `/models` and `Product`/`Offer` schema on model detail pages
- `sitemap.xml` generated at build time (static routes) + dynamic model routes
- `robots.txt` allowing all crawlers
- Open Graph tags on all pages (model pages include current price in OG description)

#### Security
- Server-side API key (`LLM_PRICING_API_KEY`) used in SSR data fetching only — never in client bundle
- Only `NEXT_PUBLIC_` prefix for genuinely public values (Lemon Squeezy checkout URLs)
- CSP, `X-Content-Type-Options`, `X-Frame-Options` headers on all responses (via `next.config.js`)
- User-supplied calculator inputs validated and clamped server-side; never trusted for pricing calculations
- No user PII collected in Phase 3

#### Accessibility
- WCAG 2.1 AA for all interactive elements
- Keyboard navigable modals, filters, and forms
- `aria-label` on icon-only buttons

---

## Design (locked — see `.claude/frontend-design-spec.md`)

| Concern | Decision |
|---|---|
| Visual inspiration | Greptile.com — clean isometric, warm-light, borders not shadows |
| Color palette | Warm bone/ivory base, amber accent (`#D97706`), semantic green/red/blue |
| Typography | Orbitron (numbers/prices/mono), Outfit (all other text) |
| Components | shadcn/ui |
| Depth | Borders only — no box-shadows, no backdrop-filter |
| Theme | Single fixed light theme — no dark/light toggle |
| Isometric language | Running theme across hero, pricing cards, calculator, comparison table, charts |
| Vibe | Gamified + agentic — tier progression, game-event notifications, agent-optimized badges |

---

## Success Criteria

| Metric | Target |
|---|---|
| Lighthouse performance score | ≥ 90 on all pages |
| Lighthouse SEO score | ≥ 95 |
| LCP | < 2.5s (mobile, 4G) |
| CLS | < 0.1 |
| Models page renders correct data | 100% match with `GET /v1/models` response |
| Cost calculator accuracy | 0% deviation from formula: `(input_tokens / 1M) * input_price_per_m + (output_tokens / 1M) * output_price_per_m` |
| Pricing CTA links | All CTAs point to correct Lemon Squeezy checkout URLs |
| No API key in client bundle | Confirmed by `next build` output analysis |
| All pages SSR | Verified by disabling JS in browser — content still visible |

---

## Constraints & Assumptions

- **API is live**: Phase 2 REST API is deployed and reachable at `NEXT_PUBLIC_API_BASE_URL` (server-side calls use `LLM_PRICING_API_BASE_URL`)
- **No web auth in Phase 3**: All pages are fully public; dashboard (API key management, usage stats) is a future phase
- **Lemon Squeezy checkout**: Checkout URLs are pre-created in Lemon Squeezy and passed as env vars — no in-app payment processing
- **Hero Blender scene**: Requires Blender running locally with the blender-mcp addon; fallback is LottieFiles animation
- **No i18n**: English only in Phase 3
- **Railway deployment**: Frontend deploys as a separate Railway service alongside the Go API

---

## Out of Scope (Phase 3)

- Authenticated dashboard (API key creation, usage stats, webhook management) — future phase
- User accounts / login / sign-up flows
- Model recommender UI (deferred to Phase 4)
- `/v1/ask` natural language query UI
- SSE stream UI (`/v1/stream/changes` viewer)
- Admin/review queue for flagged price discrepancies
- Email notifications or webhook configuration UI
- Mobile app

---

## Dependencies

| Dependency | Type | Notes |
|---|---|---|
| Phase 2 REST API | Internal | Must be deployed; all pages depend on it |
| Unkey | External | API key validation (server-side only) |
| Lemon Squeezy | External | Checkout URLs for pricing page CTAs |
| Google Fonts CDN | External | Orbitron + Outfit |
| Blender + blender-mcp | Dev tooling | Hero scene generation only |
| Railway | Hosting | Separate frontend service |

---

## Technical Notes

### Data Fetching Strategy
- SSR pages fetch data in `generateStaticParams` (static model list at build) or `async` server components
- Dynamic data (prices, changes) uses `next/cache` with `revalidate: 300` (5 min) for balance of freshness and performance
- `/changes` page uses client-side polling at 60s interval after initial SSR render
- Server-side API calls use `LLM_PRICING_API_KEY` env var — never exposed to client

### URL State
- Filters on `/models` and `/compare` are URL-searchParam based for shareability and SSR compatibility
- Model detail modal uses `?model=<id>` query param — deep-linkable and crawlable

### Hero GLB Delivery
- Blender MCP exports GLB + WebP preview render
- WebP serves as above-the-fold static image (fast)
- GLB loaded lazily via `<Canvas>` (react-three-fiber or `<model-viewer>`) after LCP
