# Phase: frontend
> Build the public-facing Next.js 15 (App Router + SSR) frontend — model browser, cost calculator, comparison page, price change feed, pricing page, and hero Blender scene.

**Branch:** `epic/frontend` | **Worktree:** `../epic-frontend` | **Created:** 2026-02-18T21:30:05Z

---

## Current Status

| # | Task | Status | GitHub |
|---|------|--------|--------|
| #24 | Project Scaffolding & Design System | **done** | [#24](https://github.com/romeo-folie/llm-pricing-api/issues/24) |
| #27 | Hero Blender MCP Scene | **done** | [#27](https://github.com/romeo-folie/llm-pricing-api/issues/27) |
| #23 | Model Browser, Detail Modal & History | **done** | [#23](https://github.com/romeo-folie/llm-pricing-api/issues/23) |
| #25 | Price Change Feed | **done** | [#25](https://github.com/romeo-folie/llm-pricing-api/issues/25) |
| #26 | Compare Page & Cost Calculator | **done** | [#26](https://github.com/romeo-folie/llm-pricing-api/issues/26) |
| #28 | Pricing Page | **done** | [#28](https://github.com/romeo-folie/llm-pricing-api/issues/28) |
| #29 | Landing Page | **done** | [#29](https://github.com/romeo-folie/llm-pricing-api/issues/29) |
| #30 | SEO, Metadata, Sitemap & Deployment | **open** | [#30](https://github.com/romeo-folie/llm-pricing-api/issues/30) |

**Next action:** Start **#30 (SEO, Metadata, Sitemap & Deployment)** — generateMetadata on all pages, JSON-LD schemas, sitemap.ts, robots.txt, OG images, Lighthouse audit, Railway deployment config.

---

## Goals

- Next.js 15 App Router with SSR for SEO
- Isometric design language (bone/ivory palette, amber accent, Orbitron + Outfit fonts, borders-not-shadows)
- Model browser with filters, detail modal, price history chart
- Cost calculator and comparison page with shareable URL params
- Price change feed with 60s client-side polling
- Pricing page with tier cards
- Lighthouse Performance ≥ 90, SEO ≥ 95
- Railway deployment as a new `frontend` service

---

## Architecture Notes

- All work lives in `frontend/` — this is a Next.js 16 project within the monorepo root
- Design tokens are locked in `.claude/frontend-design-spec.md` — read it before writing any CSS or components
- **No box-shadows anywhere** — use `border border-[--border]` for component edges per the spec
- API client (`lib/api.ts`) is a **server-only** module — it injects `LLM_PRICING_API_KEY` for Dev-tier endpoints; this key must never reach the client bundle
- Filters and calculator inputs live in URL search params — all user state must be shareable via URL
- `next: { revalidate: 300 }` on all fetches — do not use `cache: 'no-store'` except in the changes feed poller
- shadcn/ui components in `frontend/components/ui/`; design utilities in `frontend/app/globals.css`
- `npm run build` must pass before any commit

---

## What Was Done

### Session 2026-02-18 (Session 1 + 2)

- Created `.claude/epics/frontend/` directory with epic.md and all task files (#23–#30)
- **#24 closed**: Next.js 16 scaffold, Tailwind v4 @theme design tokens, shadcn/ui (8 components), `lib/api.ts` (server-only typed client, lazy `buildHeaders()`, RFC 7807 error parsing), Nav + Footer layout (with `CopyrightYear` client component), security headers (CSP, X-Frame, X-Content-Type), Railway config, `/api/health` route, `.env.example`, README
- **#27 Stream A closed**: HeroScene.tsx (model-viewer, lazy-loaded, split into ModelViewerHero + HeroScene for Rules of Hooks), HeroFallback.tsx (@lottiefiles/react-lottie-player, useId() SVG dedup), types/model-viewer.d.ts
- **#23 closed**: SSR model browser with URL filters, ModelCard list, ModelDetailModal (?model= deep-link), PriceHistoryChart (Recharts, 7d/30d/90d/All), /models/[id] detail page, JSON-LD Product schema
- **#25 closed**: Price change feed with SSR initial data, 60s polling via /api/changes proxy, animate-flash on new items, LIVE/PAUSED badge, provider + date URL filters
- **#26 closed**: Compare page (ModelPicker, CompareTable, best-value highlight, share URL) + cost calculator (server action calculateCost, daily/monthly/yearly toggle, URL state)
- **#28 closed**: Static pricing page with 3 tier cards (Recruit/Engineer/Architect), rank progression strip, FAQ Accordion
- All 4 Sonnet + 2 Opus code review passes completed; all findings fixed
- **#27 Stream B closed**: Blender MCP scene built (3 server racks + 5 gold coins + 3-point lighting). hero.webp (15KB, 1200×800, Cycles 128 samples) + hero.glb (93KB) in frontend/public/

---

## Key Files

| Path | Role |
|------|------|
| `frontend/` | Next.js 16 project root (primary work area) |
| `frontend/app/` | App Router pages and layouts |
| `frontend/app/globals.css` | Tailwind v4 @theme + all 23 design tokens |
| `frontend/components/layout/` | Nav.tsx + Footer.tsx |
| `frontend/components/ui/` | shadcn/ui primitives |
| `frontend/components/hero/` | HeroScene.tsx + HeroFallback.tsx |
| `frontend/components/model/` | ModelCard, ModelDetailModal, ModelPicker, PriceHistoryChart |
| `frontend/components/compare/` | CompareClient, CompareTable, ModelPicker |
| `frontend/components/changes/` | ChangesFeed, ChangeRow |
| `frontend/components/calculator/` | CalculatorClient |
| `frontend/app/calculator/actions.ts` | Server action for cost calculation |
| `frontend/lib/api.ts` | Typed server-side API client (server-only) |
| `frontend/next.config.ts` | CSP + security headers |
| `.claude/frontend-design-spec.md` | **Locked design language — read before writing any UI** |
| `.claude/epics/frontend/epic.md` | Full epic spec with task order |

---

## CCPM Quick Commands

```text
/pm:next                       # What to work on next
/pm:issue-start 29             # Landing page (unblocked once #27 Stream B done)
/pm:issue-start 30             # SEO + deployment (unblocked once #23-#29 done)
/pm:epic-status frontend       # This epic's progress
```

---

## Resuming Work

1. Check the **Current Status** table above for the next `open` task without blockers.
2. Run `/pm:issue-start <N>` to claim it and read its full spec.
3. Before writing any UI code, invoke `/frontend-design:frontend-design` per CLAUDE.md.
4. Run `npm run build --prefix frontend` — TypeScript must be error-free before committing.
5. Run `/code-reviewer` (Sonnet) then `/code-reviewer` (Opus) — fix all findings before closing.
6. Run `/pm:issue-close <N>` to mark complete and update the status table in this file.
7. Remaining order: **#30 (SEO/deploy)** — final task.
