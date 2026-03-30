"use client";

import type { CSSProperties } from "react";
import { useEffect, useId, useState } from "react";
import { OrbitalReactor, ORBITAL_REACTOR_OUTER_RX } from "./OrbitalReactor";

/* ─── Static data ──────────────────────────────────────────────────────────── */

interface SourceNode {
  name: string;
  sub: string;
  /** Centre in viewBox coordinates (constellation layout). */
  cx: number;
  cy: number;
  tier: "primary" | "aggregator";
  /** Pre-computed index within the tier — used for stable animation timing. */
  tierIdx: number;
}

/**
 * Constellation layout — glowing planet nodes.
 *
 * Primary sources sit closer to the reactor in a loose triangle, larger and
 * brighter. Aggregators are scattered wider with a dimmer, smaller glow —
 * like distant stars orbiting the main cluster.
 */
const SOURCES: SourceNode[] = [
  // Primary — tight cluster; scraped @every 24h (daily)
  { name: "OpenAI", sub: "daily", cx: 100, cy: 72, tier: "primary", tierIdx: 0 },
  { name: "Anthropic", sub: "daily", cx: 135, cy: 170, tier: "primary", tierIdx: 1 },
  { name: "Google", sub: "daily", cx: 105, cy: 268, tier: "primary", tierIdx: 2 },
  // Aggregators — scattered wider; scrape cadences vary
  { name: "OpenRouter", sub: "every 6h", cx: 210, cy: 32, tier: "aggregator", tierIdx: 0 },
  { name: "LiteLLM", sub: "daily", cx: 255, cy: 215, tier: "aggregator", tierIdx: 1 },
  { name: "Hugging Face", sub: "daily", cx: 220, cy: 285, tier: "aggregator", tierIdx: 2 },
];

/** Per-provider brand colors applied to planet core, glow, and flow particle. */
const PLANET_COLOR: Record<string, string> = {
  "OpenAI": "#17BECF",
  "Anthropic": "#D4A574",
  "Google": "#4285F4",
  "OpenRouter": "#B47AEA",
  "LiteLLM": "#00E5FF",
  "Hugging Face": "#FFD21E",
};

interface Endpoint {
  method: "GET" | "POST";
  path: string;
}

const ENDPOINTS: Endpoint[] = [
  { method: "GET", path: "/v1/models" },
  { method: "GET", path: "/v1/history" },
  { method: "POST", path: "/v1/stream" },
  { method: "GET", path: "/v1/context" },
];

/* ─── Planet radii ─────────────────────────────────────────────────────────── */

const PRIMARY_R = 8;
const AGGREGATOR_R = 4.5;

/* ─── Layout constants ─────────────────────────────────────────────────────── */

const VW = 680;
const VH = 340;

// Reactor center
const RCX = 380;
const RCY = Math.round(VH / 2); // 170

// Endpoint cards
const E_X = 540;
const E_W = 120;
const E_H = 36;
const E_DY = 44;
const E_Y0 = RCY - E_H / 2 - E_DY * 1.5;

/* ─── Helpers ──────────────────────────────────────────────────────────────── */

function endpointY(i: number) { return E_Y0 + i * E_DY; }

/**
 * Reactor connection offset: derived from OrbitalReactor's outer ellipse
 * (rx=56, see OrbitalReactor.tsx) multiplied by the scale prop used below (1.1).
 * Computed here so any change to either value propagates automatically.
 */
const REACTOR_OUTER_RX = ORBITAL_REACTOR_OUTER_RX; // imported from OrbitalReactor — single source of truth
const REACTOR_SCALE = 1.1;                         // scale prop passed to OrbitalReactor
const REACTOR_EDGE = Math.round(REACTOR_OUTER_RX * REACTOR_SCALE); // ≈ 62

/** Smooth cubic bezier from planet edge to reactor left edge. */
function curvedPath(sx: number, sy: number, r: number): string {
  const startX = sx + r;
  const startY = sy;
  const endX = RCX - REACTOR_EDGE;
  const endY = RCY;
  const midX = startX + (endX - startX) * 0.5;
  return `M ${startX},${startY} C ${midX},${startY} ${midX},${endY} ${endX},${endY}`;
}

/* ─── Component ────────────────────────────────────────────────────────────── */

interface HeroSceneProps {
  className?: string;
  style?: CSSProperties;
}

export default function HeroScene({ className, style }: HeroSceneProps) {
  const uid = useId();

  const [mounted, setMounted] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    setMounted(true);
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReducedMotion(mq.matches);
    const handler = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  return (
    <div
      className={className}
      style={{ width: "100%", overflow: "visible", ...style }}
      role="img"
      aria-label="Architecture diagram: direct API pricing data from OpenAI, Anthropic, and Google, plus aggregated data from OpenRouter, LiteLLM, and Hugging Face, flows through a reconciliation engine to verified API endpoints"
    >
      <div
        className="hero-svg-wrap"
        style={{
          width: "100%",
          display: "flex",
          justifyContent: "center",
          overflow: "visible"
        }}
      >
        <svg
          viewBox={`0 0 ${VW} ${VH}`}
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
          className="hero-svg-main"
          style={{
            width: "100%",
            height: "auto",
            display: "block",
            overflow: "visible",
            flexShrink: 0,
            transform: "scale(1.2)",
            transformOrigin: "center center"
          }}
        >
          {/* Shift slightly left, letting it lean right as requested (380 -> 370) */}
          <g transform="translate(-20, 0)">
            {/* ── Glow definitions ──────────────────────────────────────── */}
            {/* ── Flow lines (planet → reactor) ─────────────────────────── */}
            {SOURCES.map((s) => {
              const isPrimary = s.tier === "primary";
              const r = isPrimary ? PRIMARY_R : AGGREGATOR_R;
              const d = curvedPath(s.cx, s.cy, r);
              const planetColor = PLANET_COLOR[s.name] ?? "var(--green)";
              const dur = isPrimary
                ? `${2.4 + s.tierIdx * 0.3}s`
                : `${3.0 + s.tierIdx * 0.4}s`;

              return (
                <g key={`flow-${s.name}`}>
                  <path
                    d={d}
                    stroke="var(--borderDk)"
                    strokeWidth={isPrimary ? 1 : 0.75}
                    strokeDasharray={isPrimary ? "6 4" : "3 5"}
                    opacity={isPrimary ? 0.4 : 0.25}
                    fill="none"
                  />
                  <path
                    d={d}
                    stroke={planetColor}
                    strokeWidth={isPrimary ? 1.2 : 0.6}
                    strokeDasharray={isPrimary ? "4 8" : "3 6"}
                    strokeOpacity={isPrimary ? 0.5 : 0.25}
                    fill="none"
                    className="hero-dash"
                  />
                  {mounted && !reducedMotion && (
                    <circle r={isPrimary ? 2.5 : 1.8} fill={planetColor} opacity={isPrimary ? 0.85 : 0.55}>
                      <animateMotion dur={dur} repeatCount="indefinite" path={d} />
                    </circle>
                  )}
                </g>
              );
            })}

            {/* ── Planet nodes (sources) ────────────────────────────────── */}
            {SOURCES.map((s) => {
              const isPrimary = s.tier === "primary";
              const r = isPrimary ? PRIMARY_R : AGGREGATOR_R;
              const glowR = isPrimary ? 24 : 14;
              const planetColor = PLANET_COLOR[s.name] ?? "var(--green)";
              // Stagger pulse phase per node so they don't breathe in sync.
              const pulseDur = isPrimary ? "3.5s" : "4.5s";
              const pulseDelay = `${s.tierIdx * -1.2}s`;
              // Per-provider glow gradient IDs (unique per render via uid prefix).
              const gradId = `${uid}-glow-${s.name.replace(/\s+/g, "-").toLowerCase()}`;

              return (
                <g key={`planet-${s.name}`}>
                  {/* Per-provider radial glow gradient */}
                  <defs>
                    <radialGradient id={gradId}>
                      <stop offset="0%" stopColor={planetColor} stopOpacity={isPrimary ? 0.45 : 0.25} />
                      <stop offset="40%" stopColor={planetColor} stopOpacity={isPrimary ? 0.12 : 0.06} />
                      <stop offset="100%" stopColor={planetColor} stopOpacity="0" />
                    </radialGradient>
                  </defs>
                  {/* Glow halo */}
                  <circle
                    cx={s.cx} cy={s.cy} r={glowR}
                    fill={`url(#${gradId})`}
                    className="planet-glow"
                    style={{
                      "--planet-pulse-dur": pulseDur,
                      "--planet-pulse-delay": pulseDelay,
                      transformOrigin: `${s.cx}px ${s.cy}px`,
                    } as CSSProperties}
                  />
                  {/* Bright dot core */}
                  <circle
                    cx={s.cx} cy={s.cy} r={r}
                    fill={planetColor}
                    opacity={isPrimary ? 0.95 : 0.55}
                  />
                  {/* Name label */}
                  <text
                    x={s.cx} y={s.cy + r + 14}
                    fontSize={isPrimary ? 10 : 8.5}
                    fontWeight="600"
                    fontFamily="var(--font-geist-sans), sans-serif"
                    fill="var(--hero-label, var(--ink))"
                    textAnchor="middle"
                  >
                    {s.name}
                  </text>
                  {/* Subtitle */}
                  <text
                    x={s.cx} y={s.cy + r + (isPrimary ? 25 : 23)}
                    fontSize={isPrimary ? 7.5 : 6.5}
                    fontFamily="var(--font-geist-mono), monospace"
                    fill={planetColor}
                    textAnchor="middle"
                    opacity="0.8"
                  >
                    {s.sub}
                  </text>
                </g>
              );
            })}

            {/* ── Orbital Reactor (engine) ──────────────────────────────── */}
            <OrbitalReactor cx={RCX} cy={RCY} scale={REACTOR_SCALE} />

            {/* Reactor label */}
            <text
              x={RCX} y={RCY + 72}
              fontSize="8.5" fontFamily="var(--font-geist-mono), monospace"
              fill="var(--accent)" textAnchor="middle" letterSpacing="0.5" opacity="0.85"
            >
              reconcile · 6-source
            </text>

            {/* ── Output flow lines: reactor → endpoints ────────────────── */}
            {ENDPOINTS.map((_ep, i) => {
              const ey = endpointY(i) + E_H / 2;
              const fanY = RCY - 15 + i * 10;
              const startX = RCX + REACTOR_EDGE;
              const midX = startX + (E_X - startX) * 0.5;
              const d = `M ${startX},${fanY} C ${midX},${fanY} ${midX},${ey} ${E_X},${ey}`;
              return (
                <g key={`out-${i}`}>
                  <path d={d} stroke="var(--borderDk)" strokeWidth="1" strokeDasharray="4 4" fill="none" opacity="0.4" />
                  <path d={d} stroke="var(--green)" strokeWidth="1" strokeDasharray="4 8" strokeOpacity="0.45" fill="none" className="hero-dash" />
                  {mounted && !reducedMotion && (
                    <circle r="2.5" fill="var(--green)" opacity="0.85">
                      <animateMotion dur={`${2.5 + i * 0.3}s`} repeatCount="indefinite" path={d} />
                    </circle>
                  )}
                </g>
              );
            })}

            {/* ── API label ─────────────────────────────────────────────── */}
            <text
              x={E_X + E_W / 2} y={E_Y0 - 8}
              fontSize="8" fontWeight="600"
              fontFamily="var(--font-geist-mono), monospace"
              fill="var(--hero-label, var(--dim))" textAnchor="middle" letterSpacing="1"
            >
              API
            </text>

            {/* ── Endpoint cards ────────────────────────────────────────── */}
            {ENDPOINTS.map((ep, i) => {
              const y = endpointY(i);
              const cy = y + E_H / 2;
              // Method pill geometry
              const pillW = ep.method === "POST" ? 30 : 24;
              const pillH = 13;
              const pillX = E_X + 8;
              const pillY = cy - pillH / 2;
              // GET → blue pill; POST → green pill — both readable on light & dark bg
              const pillBg = ep.method === "POST" ? "var(--hero-method-post-bg,   #B9EDD8)" : "var(--hero-method-get-bg,  #B5D4F4)";
              const pillText = ep.method === "POST" ? "var(--hero-method-post-text, #0C5E3A)" : "var(--hero-method-get-text, #0C447C)";
              const pathX = pillX + pillW + 6;
              return (
                <g key={ep.path}>
                  <rect
                    x={E_X} y={y} width={E_W} height={E_H} rx="4"
                    fill="var(--hero-endpoint-bg, var(--surfaceHi))"
                    stroke="var(--hero-endpoint-border, var(--border))"
                    strokeWidth="1"
                  />
                  {/* Method pill */}
                  <rect x={pillX} y={pillY} width={pillW} height={pillH} rx="3" fill={pillBg} />
                  <text
                    x={pillX + pillW / 2} y={cy}
                    fontSize="7.5" fontWeight="700"
                    fontFamily="var(--font-geist-mono), monospace"
                    fill={pillText} textAnchor="middle" dominantBaseline="middle"
                  >
                    {ep.method}
                  </text>
                  {/* Route path */}
                  <text
                    x={pathX} y={cy}
                    fontSize="9" fontWeight="500"
                    fontFamily="var(--font-geist-mono), monospace"
                    fill="var(--hero-endpoint-text, var(--ink))"
                    dominantBaseline="middle"
                  >
                    {ep.path}
                  </text>
                </g>
              );
            })}

            {/* Stats footnote */}
            <text
              x={RCX} y={VH - 8}
              fontSize="8.5" fontFamily="var(--font-geist-mono), monospace"
              fill="var(--hero-label, var(--dim))" textAnchor="middle" opacity="0.85"
            >
              6 sources · 2,330 models · &lt;60s latency
            </text>

          </g>
        </svg>
      </div>
    </div>
  );
}
