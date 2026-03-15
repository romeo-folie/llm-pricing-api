"use client";

import type { CSSProperties } from "react";
import { useEffect, useState } from "react";
import { OrbitalReactor } from "./OrbitalReactor";

/* ─── Static data ──────────────────────────────────────────────────────────── */

interface SourceNode {
  name: string;
  sub: string;
  /** Centre in viewBox coordinates (constellation layout). */
  cx: number;
  cy: number;
  tier: "primary" | "aggregator";
}

/**
 * Constellation layout — glowing planet nodes.
 *
 * Primary sources sit closer to the reactor in a loose triangle, larger and
 * brighter. Aggregators are scattered wider with a dimmer, smaller glow —
 * like distant stars orbiting the main cluster.
 */
const SOURCES: SourceNode[] = [
  // Primary — tight cluster
  { name: "OpenAI",    sub: "direct",  cx: 100, cy: 72,  tier: "primary" },
  { name: "Anthropic", sub: "direct",  cx: 135, cy: 170, tier: "primary" },
  { name: "Google",    sub: "direct",  cx: 90,  cy: 268, tier: "primary" },
  // Aggregators — scattered wider
  { name: "OpenRouter",   sub: "6h",    cx: 210, cy: 32,  tier: "aggregator" },
  { name: "LiteLLM",      sub: "daily", cx: 230, cy: 135, tier: "aggregator" },
  { name: "Hugging Face", sub: "daily", cx: 220, cy: 300, tier: "aggregator" },
];

const ENDPOINTS = ["/v1/models", "/v1/history", "/v1/stream", "/v1/context"];

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

/** Smooth cubic bezier from planet edge to reactor left edge. */
function curvedPath(sx: number, sy: number, r: number): string {
  const startX = sx + r;
  const startY = sy;
  const endX = RCX - 58;
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

  // Per-tier indices for stable animation durations.
  const primaryNodes = SOURCES.filter((s) => s.tier === "primary");
  const aggregatorNodes = SOURCES.filter((s) => s.tier === "aggregator");

  return (
    <div
      className={className}
      style={{ width: "100%", ...style }}
      role="img"
      aria-label="Architecture diagram: direct API pricing data from OpenAI, Anthropic, and Google, plus aggregated data from OpenRouter, LiteLLM, and Hugging Face, flows through a reconciliation engine to verified API endpoints"
    >
      <svg
        viewBox={`0 0 ${VW} ${VH}`}
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        style={{ width: "100%", height: "auto", display: "block" }}
      >
        {/* ── Glow definitions ──────────────────────────────────────── */}
        <defs>
          {/* Primary planet glow — subtle ambient, not self-luminous */}
          <radialGradient id="glow-primary">
            <stop offset="0%"  stopColor="var(--green)"  stopOpacity="0.45" />
            <stop offset="40%" stopColor="var(--green)"  stopOpacity="0.12" />
            <stop offset="100%" stopColor="var(--green)" stopOpacity="0" />
          </radialGradient>
          {/* Aggregator planet glow — barely visible ambient */}
          <radialGradient id="glow-aggregator">
            <stop offset="0%"  stopColor="var(--green)"  stopOpacity="0.25" />
            <stop offset="40%" stopColor="var(--green)"  stopOpacity="0.06" />
            <stop offset="100%" stopColor="var(--green)" stopOpacity="0" />
          </radialGradient>
        </defs>

        {/* ── Flow lines (planet → reactor) ─────────────────────────── */}
        {SOURCES.map((s) => {
          const isPrimary = s.tier === "primary";
          const r = isPrimary ? PRIMARY_R : AGGREGATOR_R;
          const d = curvedPath(s.cx, s.cy, r);
          const baseColor = "var(--green)";
          const tierIdx = isPrimary
            ? primaryNodes.indexOf(s)
            : aggregatorNodes.indexOf(s);
          const dur = isPrimary
            ? `${2.4 + tierIdx * 0.3}s`
            : `${3.0 + tierIdx * 0.4}s`;

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
                stroke={baseColor}
                strokeWidth={isPrimary ? 1.2 : 0.6}
                strokeDasharray={isPrimary ? "4 8" : "3 6"}
                strokeOpacity={isPrimary ? 0.5 : 0.25}
                fill="none"
                className="hero-dash"
              />
              {mounted && !reducedMotion && (
                <circle r={isPrimary ? 2.5 : 1.8} fill={baseColor} opacity={isPrimary ? 0.85 : 0.55}>
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
          const tierIdx = isPrimary
            ? primaryNodes.indexOf(s)
            : aggregatorNodes.indexOf(s);
          // Stagger pulse phase per node so they don't breathe in sync.
          const pulseDur = isPrimary ? "3.5s" : "4.5s";
          const pulseDelay = `${tierIdx * -1.2}s`;

          return (
            <g key={`planet-${s.name}`}>
              {/* Glow halo — no container, just radial gradient */}
              <circle
                cx={s.cx} cy={s.cy} r={glowR}
                fill={isPrimary ? "url(#glow-primary)" : "url(#glow-aggregator)"}
                style={mounted && !reducedMotion ? {
                  animation: `planetPulse ${pulseDur} ease-in-out infinite`,
                  animationDelay: pulseDelay,
                  transformOrigin: `${s.cx}px ${s.cy}px`,
                } : undefined}
              />
              {/* Bright dot core — no ring, no fill container */}
              <circle
                cx={s.cx} cy={s.cy} r={r}
                fill="var(--green)"
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
                fill={isPrimary ? "var(--green)" : "var(--muted)"}
                textAnchor="middle"
                opacity="0.8"
              >
                {s.sub}
              </text>
            </g>
          );
        })}

        {/* ── Orbital Reactor (engine) ──────────────────────────────── */}
        <OrbitalReactor cx={RCX} cy={RCY} scale={1.1} />

        {/* Reactor label */}
        <text
          x={RCX} y={RCY + 72}
          fontSize="8.5" fontFamily="var(--font-geist-mono), monospace"
          fill="var(--accent)" textAnchor="middle" letterSpacing="0.5" opacity="0.85"
        >
          reconcile · 2-source
        </text>

        {/* ── Output flow lines: reactor → endpoints ────────────────── */}
        {ENDPOINTS.map((_, i) => {
          const ey = endpointY(i) + E_H / 2;
          const fanY = RCY - 15 + i * 10;
          const startX = RCX + 58;
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
          return (
            <g key={ep}>
              <rect
                x={E_X} y={y} width={E_W} height={E_H} rx="4"
                fill="var(--hero-endpoint-bg, var(--surfaceHi))"
                stroke="var(--hero-endpoint-border, var(--border))"
                strokeWidth="1"
              />
              <text
                x={E_X + 10} y={y + E_H / 2 - 3}
                fontSize="10" fontWeight="600"
                fontFamily="var(--font-geist-mono), monospace"
                fill="var(--hero-endpoint-text, var(--ink))" dominantBaseline="middle"
              >
                {ep}
              </text>
              <text
                x={E_X + 10} y={y + E_H / 2 + 10}
                fontSize="8" fontWeight="600"
                fontFamily="var(--font-geist-mono), monospace"
                fill="var(--green)" dominantBaseline="middle" opacity="0.8"
              >
                endpoint
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

        {/* Pulse animation for planet glow halos */}
        <style>{`
          @keyframes planetPulse {
            0%, 100% { opacity: 0.6; transform: scale(1); }
            50% { opacity: 0.85; transform: scale(1.06); }
          }
          @media (prefers-reduced-motion: reduce) {
            @keyframes planetPulse { 0%, 100% { opacity: 0.8; transform: none; } }
          }
        `}</style>
      </svg>
    </div>
  );
}
