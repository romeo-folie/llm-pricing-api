"use client";

import type { CSSProperties } from "react";
import { useEffect, useState } from "react";
import { OrbitalReactor } from "./OrbitalReactor";

/* ─── Static data ──────────────────────────────────────────────────────────── */

interface SourceNode {
  name: string;
  sub: string;
  /** Position in viewBox coordinates (staggered constellation layout). */
  x: number;
  y: number;
  /** Card dimensions. */
  w: number;
  h: number;
  tier: "primary" | "aggregator";
}

/**
 * Constellation layout:
 *   - Primary sources (OpenAI, Anthropic, Google) clustered close to the
 *     reactor in a tight triangle, larger cards, bolder styling.
 *   - Aggregators (OpenRouter, LiteLLM, HF) scattered wider, smaller cards,
 *     dimmer/dashed styling — like distant stars in the constellation.
 *
 * The goal is organic asymmetry with clear visual hierarchy.
 */
const SOURCES: SourceNode[] = [
  // Primary — tight cluster, larger
  { name: "OpenAI",    sub: "direct",  x: 42,  y: 60,   w: 118, h: 44, tier: "primary" },
  { name: "Anthropic", sub: "direct",  x: 80,  y: 150,  w: 118, h: 44, tier: "primary" },
  { name: "Google",    sub: "direct",  x: 30,  y: 240,  w: 118, h: 44, tier: "primary" },
  // Aggregators — scattered wider, smaller
  { name: "OpenRouter",   sub: "6h",    x: 175, y: 18,   w: 100, h: 36, tier: "aggregator" },
  { name: "LiteLLM",      sub: "daily", x: 195, y: 120,  w: 100, h: 36, tier: "aggregator" },
  { name: "Hugging Face", sub: "daily", x: 185, y: 280,  w: 100, h: 36, tier: "aggregator" },
];

const ENDPOINTS = ["/v1/models", "/v1/history", "/v1/stream", "/v1/context"];

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

/** Generate a smooth cubic bezier path from source card right edge to reactor. */
function curvedPath(sx: number, sy: number, sw: number, sh: number): string {
  const startX = sx + sw;
  const startY = sy + sh / 2;
  const endX = RCX - 58; // reactor left edge
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
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReducedMotion(mq.matches);
    const handler = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

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
        {/* ── Constellation flow lines (source → reactor) ───────────── */}
        {SOURCES.map((s, i) => {
          const d = curvedPath(s.x, s.y, s.w, s.h);
          const isPrimary = s.tier === "primary";
          const baseColor = isPrimary ? "var(--accent)" : "var(--muted)";
          // Use per-tier index for durations so timing stays stable if SOURCES ordering changes.
          const primaryIdx  = SOURCES.filter((n) => n.tier === "primary").indexOf(s);
          const aggregatorIdx = SOURCES.filter((n) => n.tier === "aggregator").indexOf(s);
          const dur = isPrimary
            ? `${2.4 + primaryIdx * 0.3}s`
            : `${3.0 + aggregatorIdx * 0.4}s`;

          return (
            <g key={`flow-${s.name}`}>
              {/* Ghost path */}
              <path
                d={d}
                stroke="var(--borderDk)"
                strokeWidth={isPrimary ? 1 : 0.75}
                strokeDasharray={isPrimary ? "6 4" : "3 5"}
                opacity={isPrimary ? 0.4 : 0.25}
                fill="none"
              />
              {/* Accent overlay */}
              <path
                d={d}
                stroke={baseColor}
                strokeWidth={isPrimary ? 1.2 : 0.6}
                strokeDasharray={isPrimary ? "4 8" : "3 6"}
                strokeOpacity={isPrimary ? 0.5 : 0.25}
                fill="none"
                className="hero-dash"
              />
              {/* Animated bead */}
              {!reducedMotion && (
                <circle r={isPrimary ? 2.5 : 1.8} fill={baseColor} opacity={isPrimary ? 0.85 : 0.55}>
                  <animateMotion dur={dur} repeatCount="indefinite" path={d} />
                </circle>
              )}
            </g>
          );
        })}

        {/* ── Source cards (constellation) ───────────────────────────── */}
        {SOURCES.map((s) => {
          const isPrimary = s.tier === "primary";
          return (
            <g key={s.name}>
              <rect
                x={s.x} y={s.y} width={s.w} height={s.h} rx="4"
                fill={isPrimary ? "var(--greenLt)" : "var(--accentLt)"}
                stroke={isPrimary ? "var(--green)" : "var(--accent)"}
                strokeWidth={isPrimary ? 1 : 0.75}
                strokeOpacity={isPrimary ? 0.4 : 0.2}
                strokeDasharray={isPrimary ? "none" : "3 2"}
              />
              <text
                x={s.x + 12} y={s.y + s.h / 2 - 3}
                fontSize={isPrimary ? 11 : 9.5}
                fontWeight="600"
                fontFamily="var(--font-geist-sans), sans-serif"
                fill="var(--ink)"
                dominantBaseline="middle"
              >
                {s.name}
              </text>
              <text
                x={s.x + 12} y={s.y + s.h / 2 + (isPrimary ? 11 : 9)}
                fontSize={isPrimary ? 8.5 : 7.5}
                fontFamily="var(--font-geist-mono), monospace"
                fill={isPrimary ? "var(--green)" : "var(--muted)"}
                dominantBaseline="middle"
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
              {!reducedMotion && (
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
      </svg>
    </div>
  );
}
