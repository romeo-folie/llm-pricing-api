# Frontend Design Spec
> Phase 3 of dev-phases-v2. All decisions below are locked unless explicitly revised.

---

## Reference Inspiration
- **Greptile.com** — primary visual inspiration. Clean, isometric, warm-light aesthetic, non-reliance on cards/shadows, uses outlines instead.

---

## Color Palette (locked)

```css
/* Base */
--bg:          #F2EDE8;   /* warm bone, page background */
--surface:     #FDFAF7;   /* lifted ivory, cards */
--surfaceLo:   #EDE8E2;   /* recessed, inset panels */
--surfaceHi:   #FFFFFF;   /* pure white, topmost surfaces */

/* Borders */
--border:      #DDD7D0;   /* default border */
--borderDk:    #C8C0B6;   /* emphasized border */

/* Text */
--text:        #1C1917;   /* near-black, warm — body text */
--muted:       #78716C;   /* warm gray — secondary text */
--dim:         #A8A29E;   /* lighter warm gray — labels/timestamps */
--ink:         #1E293B;   /* deep slate — headings + logo */

/* Accent (single pop color) */
--accent:      #D97706;   /* amber-600 */
--accentLt:    #FEF3C7;   /* amber-100, tint backgrounds */
--accentDk:    #92400E;   /* amber-800, pressed/dark states */

/* Semantic */
--green:       #059669;   /* confirmed / positive */
--greenLt:     #D1FAE5;
--red:         #DC2626;   /* flagged / negative */
--redLt:       #FEE2E2;
--yellow:      #D97706;   /* warning (same as accent) */
--yellowLt:    #FEF3C7;
--blue:        #2563EB;   /* informational / API metrics */
--blueLt:      #DBEAFE;
--purple:      #7C3AED;   /* secondary data (Mistral, LiteLLM) */
--purpleLt:    #EDE9FE;
```

---

## Typography

| Role | Font | Fallback |
|---|---|---|
| Numbers / mono / prices | **Orbitron** | monospace |
| All other text | **Outfit** | Space Mono, sans-serif |

Both loaded from Google Fonts.

---

## Component & Styling Principles

- **shadcn/ui** for all UI primitives
- **No box-shadows** — depth is created exclusively through isometric geometry and border lines
- **No blur/backdrop-filter cards** — clean outlined surfaces only
- **Borders over elevation** — use `--border` / `--borderDk` to define component edges
- **Isometric outlines** as the primary visual language (following Greptile's approach)
- Single fixed theme — **no dark/light toggle**

---

## Isometric Design Language

Isometric geometry is a **running theme**, not just a hero element:

| Section | Isometric Treatment |
|---|---|
| Hero | Full custom Blender MCP scene (server racks, data pipeline, price tokens, neon glow) |
| Pricing cards | Rendered as isometric blocks/tiles |
| Cost calculator | Styled as isometric control panel / HUD |
| Model comparison table | Rows feel like units on an isometric game board |
| Price history charts | Isometric 3D bar chart treatment |
| Section dividers | Subtle isometric grid perspective planes |

---

## Hero Graphic

- **Method**: Custom Blender MCP scene
- **Blender MCP repo**: `github.com/ahujasid/blender-mcp`
- **Config**: `.mcp.json` already present in project root (uses `uvx blender-mcp`)
- **Scene brief**: Isometric perspective, server racks + floating price tokens + glowing data pipeline elements, neon glow lighting that complements the amber/warm palette, export as GLB + render WebP preview
- **Fallback**: LottieFiles "Isometric AI Animation" (`lottiefiles.com/free-animation/isometric-ai-animation-ztUY20dte4`)

---

## Vibe

- **Gamified + agentic** — the site should feel built for AI agents and developers who treat tooling like a game
- Tier progression (Free → Dev → Pro) mapped to achievement/rank visual language
- Agent-facing endpoints (`/v1/context`, `/v1/ask`, SSE stream) get distinct "⚡ Agent-optimized" treatment
- Real-time price changes styled as game event notifications
- Model recommender feels like a character select / roster screen
- API key dashboard: stats panel aesthetic with req count, tier, rate limit as progress bars
- "Last updated" indicators styled as LIVE badges

---

## Stack

| Layer | Choice |
|---|---|
| Framework | Next.js (SSR for SEO) |
| Language | TypeScript |
| Styling | Tailwind CSS |
| Components | shadcn/ui |
| Charts | Tremor or Recharts |
| Hero | Blender MCP → GLB / WebP |
| Fonts | Google Fonts (Orbitron + Outfit) |

---

## Phase 3 Deliverables (from dev-phases-v2.docx)

- Comparison table (side-by-side model pricing)
- Cost calculator (user inputs token counts → sees cost)
- Price history charts
- SSR for SEO
- All pages consume the REST API from Phase 2

---

## Resuming Work

1. Read this file for full design context
2. Check `.mcp.json` is present in project root (Blender MCP config)
3. Verify Blender is running with addon connected before hero scene work
4. Run `/pm:prd-new frontend` to start the epic planning
5. Invoke `frontend-design:frontend-design` skill before writing any frontend code
