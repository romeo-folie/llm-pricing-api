"use client";

import type { CSSProperties } from "react";
import { useEffect, useState } from "react";
import { WireframeCube } from "./WireframeCube";

/* ─── Static data ──────────────────────────────────────────────────────────── */

const PRIMARY_SOURCES = [
  { name: "OpenAI",    sub: "daily" },
  { name: "Anthropic", sub: "daily" },
  { name: "Google",    sub: "daily" },
];

const AGGREGATOR_SOURCES = [
  { name: "OpenRouter",    sub: "every 6h" },
  { name: "LiteLLM",      sub: "daily" },
  { name: "Hugging Face",  sub: "daily" },
];

const ENDPOINTS = ["/v1/models", "/v1/history", "/v1/stream", "/v1/context"];

/* ─── Layout constants ─────────────────────────────────────────────────────── */

// viewBox
const VW = 680;
const VH = 340;

// Source cards
const S_X  = 40;    // left edge
const S_W  = 116;   // card width
const S_H  = 42;    // card height
const S_DY = 52;    // row pitch

const P_Y0 = 26;    // first primary y
const A_Y0 = 184;   // first aggregator y

// Flow merge points
const M_X  = 220;   // x of merge dots
const MP_Y = P_Y0 + S_H / 2 + S_DY;   // primary merge y  ≈ 99
const MA_Y = A_Y0 + S_H / 2 + S_DY;   // aggregator merge y ≈ 257

// Engine / reactor center
const RCX = 380;
const RCY = Math.round(VH / 2);  // 170

// Endpoint cards
const E_X = 540;
const E_W = 120;
const E_H = 36;
const E_DY = 44;
const E_Y0 = RCY - E_H / 2 - E_DY * 1.5;  // centres 4 cards around reactor

/* ─── Helpers ──────────────────────────────────────────────────────────────── */

function primaryY(i: number)     { return P_Y0 + i * S_DY; }
function aggregatorY(i: number)  { return A_Y0 + i * S_DY; }
function endpointY(i: number)    { return E_Y0 + i * E_DY; }

function sourceCardCY(y: number) { return y + S_H / 2; }

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
        {/* ── Column labels ─────────────────────────────────────────────── */}
        <text
          x={S_X + S_W / 2} y={12}
          fontSize="8" fontWeight="600"
          fontFamily="var(--font-geist-mono), monospace"
          fill="var(--dim)" textAnchor="middle" letterSpacing="1"
        >
          SOURCES
        </text>
        <text
          x={E_X + E_W / 2} y={12}
          fontSize="8" fontWeight="600"
          fontFamily="var(--font-geist-mono), monospace"
          fill="var(--dim)" textAnchor="middle" letterSpacing="1"
        >
          API
        </text>

        {/* ── Tier labels (removed per design update) ──────────────────── */}

        {/* ── Primary source → merge flow lines ─────────────────────────── */}
        {PRIMARY_SOURCES.map((_, i) => {
          const cy = sourceCardCY(primaryY(i));
          const d = `M ${S_X + S_W},${cy} L ${M_X},${cy}`;
          return (
            <g key={`pf-${i}`}>
              <path d={d} stroke="var(--borderDk)" strokeWidth="1" strokeDasharray="4 4" />
              <path d={d} stroke="var(--accent)" strokeWidth="1" strokeDasharray="4 8" strokeOpacity="0.55" className="hero-dash" />
              {!reducedMotion && (
                <circle r="2.5" fill="var(--accent)" opacity="0.9">
                  <animateMotion dur={`${2.6 + i * 0.4}s`} repeatCount="indefinite" path={d} />
                </circle>
              )}
            </g>
          );
        })}

        {/* Vertical primary merge connector */}
        <line
          x1={M_X} y1={sourceCardCY(primaryY(0))}
          x2={M_X} y2={sourceCardCY(primaryY(2))}
          stroke="var(--accent)" strokeWidth="0.75" opacity="0.25" strokeDasharray="4 4"
        />
        <circle cx={M_X} cy={MP_Y} r="3" fill="var(--accent)" opacity="0.6" />

        {/* Primary bus → reactor left */}
        {(() => {
          const d = `M ${M_X},${MP_Y} L ${RCX - 58},${RCY}`;
          return (
            <g>
              <path d={d} stroke="var(--borderDk)" strokeWidth="1" strokeDasharray="4 4" />
              <path d={d} stroke="var(--accent)" strokeWidth="1.5" strokeDasharray="4 8" strokeOpacity="0.6" className="hero-dash" />
              {!reducedMotion && (
                <circle r="2.5" fill="var(--accent)" opacity="0.9">
                  <animateMotion dur="2.4s" repeatCount="indefinite" path={d} />
                </circle>
              )}
            </g>
          );
        })()}

        {/* ── Aggregator source → merge flow lines ──────────────────────── */}
        {AGGREGATOR_SOURCES.map((_, i) => {
          const cy = sourceCardCY(aggregatorY(i));
          const d = `M ${S_X + S_W},${cy} L ${M_X},${cy}`;
          return (
            <g key={`af-${i}`}>
              <path d={d} stroke="var(--borderDk)" strokeWidth="0.75" strokeDasharray="3 4" opacity="0.5" />
              <path d={d} stroke="var(--muted)" strokeWidth="0.75" strokeDasharray="3 6" strokeOpacity="0.35" className="hero-dash" />
            </g>
          );
        })}

        {/* Vertical aggregator merge connector */}
        <line
          x1={M_X} y1={sourceCardCY(aggregatorY(0))}
          x2={M_X} y2={sourceCardCY(aggregatorY(2))}
          stroke="var(--muted)" strokeWidth="0.5" opacity="0.2" strokeDasharray="3 4"
        />
        <circle cx={M_X} cy={MA_Y} r="2.5" fill="var(--muted)" opacity="0.45" />

        {/* Aggregator bus → reactor left */}
        {(() => {
          const d = `M ${M_X},${MA_Y} L ${RCX - 58},${RCY + 20}`;
          return (
            <g>
              <path d={d} stroke="var(--borderDk)" strokeWidth="0.75" strokeDasharray="3 4" opacity="0.5" />
              <path d={d} stroke="var(--muted)" strokeWidth="1" strokeDasharray="3 6" strokeOpacity="0.35" className="hero-dash" />
            </g>
          );
        })()}

        {/* ── Output flow lines: reactor → endpoints ────────────────────── */}
        {ENDPOINTS.map((_, i) => {
          const ey = endpointY(i) + E_H / 2;
          // Fan 4 output lines evenly from reactor right edge
          const fanY = RCY - 15 + i * 10;
          const d = `M ${RCX + 58},${fanY} L ${E_X},${ey}`;
          return (
            <g key={`out-${i}`}>
              <path d={d} stroke="var(--borderDk)" strokeWidth="1" strokeDasharray="4 4" />
              <path d={d} stroke="var(--green)" strokeWidth="1" strokeDasharray="4 8" strokeOpacity="0.45" className="hero-dash" />
              {!reducedMotion && (
                <circle r="2.5" fill="var(--green)" opacity="0.85">
                  <animateMotion dur={`${2.5 + i * 0.3}s`} repeatCount="indefinite" path={d} />
                </circle>
              )}
            </g>
          );
        })}

        {/* ── Primary source cards ──────────────────────────────────────── */}
        {PRIMARY_SOURCES.map((p, i) => {
          const y = primaryY(i);
          return (
            <g key={p.name}>
              <rect
                x={S_X} y={y} width={S_W} height={S_H} rx="3"
                fill="var(--greenLt)"
                stroke="var(--green)"
                strokeWidth="1"
                strokeOpacity="0.35"
              />
              <text
                x={S_X + 12} y={y + S_H / 2 - 3}
                fontSize="11" fontWeight="600"
                fontFamily="var(--font-geist-sans), sans-serif"
                fill="var(--ink)" dominantBaseline="middle"
              >
                {p.name}
              </text>
              <text
                x={S_X + 12} y={y + S_H / 2 + 11}
                fontSize="8.5"
                fontFamily="var(--font-geist-mono), monospace"
                fill="var(--green)" dominantBaseline="middle" opacity="0.8"
              >
                {p.sub}
              </text>
            </g>
          );
        })}

        {/* ── Aggregator source cards ───────────────────────────────────── */}
        {AGGREGATOR_SOURCES.map((p, i) => {
          const y = aggregatorY(i);
          return (
            <g key={p.name}>
              <rect
                x={S_X} y={y} width={S_W} height={S_H} rx="3"
                fill="var(--accentLt)"
                stroke="var(--accent)"
                strokeWidth="0.75"
                strokeOpacity="0.25"
                strokeDasharray="3 2"
              />
              <text
                x={S_X + 12} y={y + S_H / 2 - 3}
                fontSize="11" fontWeight="600"
                fontFamily="var(--font-geist-sans), sans-serif"
                fill="var(--ink)" dominantBaseline="middle"
              >
                {p.name}
              </text>
              <text
                x={S_X + 12} y={y + S_H / 2 + 11}
                fontSize="8.5"
                fontFamily="var(--font-geist-mono), monospace"
                fill="var(--muted)" dominantBaseline="middle"
              >
                {p.sub}
              </text>
            </g>
          );
        })}

        {/* ── Orbital Reactor (engine) ──────────────────────────────────── */}
        <WireframeCube cx={RCX} cy={RCY} />

        {/* Reactor label */}
        <text
          x={RCX} y={RCY + 68}
          fontSize="8.5" fontFamily="var(--font-geist-mono), monospace"
          fill="var(--accent)" textAnchor="middle" letterSpacing="0.5" opacity="0.85"
        >
          reconcile · 2-source
        </text>

        {/* ── Endpoint cards ────────────────────────────────────────────── */}
        {ENDPOINTS.map((ep, i) => {
          const y = endpointY(i);
          return (
            <g key={ep}>
              <rect
                x={E_X} y={y} width={E_W} height={E_H} rx="3"
                fill="var(--surfaceHi)"
                stroke="var(--border)"
                strokeWidth="1"
              />
              <text
                x={E_X + 10} y={y + E_H / 2 - 3}
                fontSize="10" fontWeight="600"
                fontFamily="var(--font-geist-mono), monospace"
                fill="var(--ink)" dominantBaseline="middle"
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
          x={VW / 2} y={VH - 8}
          fontSize="8.5" fontFamily="var(--font-geist-mono), monospace"
          fill="var(--dim)" textAnchor="middle" opacity="0.7"
        >
          6 sources · 2,330 models · &lt;60s latency
        </text>
      </svg>
    </div>
  );
}
