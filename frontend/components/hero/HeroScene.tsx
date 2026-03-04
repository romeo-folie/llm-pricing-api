import type { CSSProperties } from "react"

/* ─── Static data ──────────────────────────────────────────────────────────── */

const PROVIDERS = [
  { label: "OpenRouter", freq: "6h" },
  { label: "LiteLLM", freq: "daily" },
  { label: "Hugging Face", freq: "daily" },
]

const ENDPOINTS = [
  { path: "/v1/models" },
  { path: "/v1/history" },
  { path: "/v1/stream" },
  { path: "/v1/context" },
]

/* ─── Layout constants ─────────────────────────────────────────────────────── */

const VB_W = 540
const VB_H = 360

// Provider column (3 sources — vertically centered)
const P_X = 0
const P_W = 102
const P_H = 42
const P_GAP = 108
const P_Y0 = 54

// Reconcile box
const R_X = 192
const R_Y = 48
const R_W = 156
const R_H = 264

// Endpoint column
const E_X = VB_W - 104
const E_W = 104
const E_H = 42
const E_GAP = 78
const E_Y0 = 24

/* ─── Helpers ──────────────────────────────────────────────────────────────── */

function providerY(i: number) {
  return P_Y0 + i * P_GAP
}

function endpointY(i: number) {
  return E_Y0 + i * E_GAP
}

/** Cubic bezier from provider right-edge to reconcile left-edge */
function leftPath(i: number): string {
  const sy = providerY(i) + P_H / 2
  // 3 left-side connection points spread across the reconcile box height
  const ey = R_Y + 56 + i * 80
  const sx = P_X + P_W
  const ex = R_X
  const cx1 = sx + 36
  const cx2 = ex - 36
  return `M ${sx},${sy} C ${cx1},${sy} ${cx2},${ey} ${ex},${ey}`
}

/** Cubic bezier from reconcile right-edge to endpoint left-edge */
function rightPath(i: number): string {
  const sy = R_Y + 40 + i * 52
  const ey = endpointY(i) + E_H / 2
  const sx = R_X + R_W
  const ex = E_X
  const cx1 = sx + 36
  const cx2 = ex - 36
  return `M ${sx},${sy} C ${cx1},${sy} ${cx2},${ey} ${ex},${ey}`
}

/* ─── Mini sparkline data (mock price trend) ───────────────────────────────── */

const SPARK_POINTS = [0, 12, 8, 20, 15, 28, 22, 18, 30, 26, 34, 28]
const SPARK_W = 80
const SPARK_H = 28
function sparkline(): string {
  const max = Math.max(...SPARK_POINTS)
  return SPARK_POINTS.map((v, i) => {
    const x = (i / (SPARK_POINTS.length - 1)) * SPARK_W
    const y = SPARK_H - (v / max) * SPARK_H
    return `${i === 0 ? "M" : "L"} ${x},${y}`
  }).join(" ")
}

/* ─── Component ────────────────────────────────────────────────────────────── */

interface HeroSceneProps {
  className?: string
  style?: CSSProperties
}

export default function HeroScene({ className, style }: HeroSceneProps) {
  return (
    <div
      className={className}
      style={{
        width: "100%",
        ...style,
      }}
      role="img"
      aria-label="Architecture diagram: data from OpenRouter, LiteLLM, and Hugging Face flows through a reconciliation engine to produce verified API endpoints"
    >
      <svg
        viewBox={`0 0 ${VB_W} ${VB_H}`}
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        style={{ width: "100%", height: "auto", display: "block" }}
      >
        {/* ── Defs ────────────────────────────────────────────────────────── */}
        <defs>
          {/* Gradient for the sparkline */}
          <linearGradient id="spark-grad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.25" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* ── Connection paths (left → center) ────────────────────────────── */}
        <g>
          {PROVIDERS.map((_, i) => {
            const d = leftPath(i)
            return (
              <g key={`lp-${i}`}>
                {/* Base dashed line */}
                <path
                  d={d}
                  stroke="var(--borderDk)"
                  strokeWidth="1"
                  strokeDasharray="4 4"
                  fill="none"
                />
                {/* Animated marching overlay */}
                <path
                  d={d}
                  stroke="var(--accent)"
                  strokeWidth="1"
                  strokeDasharray="4 8"
                  strokeOpacity="0.5"
                  fill="none"
                  className="hero-dash"
                />
                {/* Traveling dot */}
                <circle r="2.5" fill="var(--accent)" opacity="0.9">
                  <animateMotion
                    dur={`${2.8 + i * 0.4}s`}
                    repeatCount="indefinite"
                    path={d}
                  />
                </circle>
              </g>
            )
          })}
        </g>

        {/* ── Connection paths (center → right) ───────────────────────────── */}
        <g>
          {ENDPOINTS.map((_, i) => {
            const d = rightPath(i)
            return (
              <g key={`rp-${i}`}>
                <path
                  d={d}
                  stroke="var(--borderDk)"
                  strokeWidth="1"
                  strokeDasharray="4 4"
                  fill="none"
                />
                <path
                  d={d}
                  stroke="var(--green)"
                  strokeWidth="1"
                  strokeDasharray="4 8"
                  strokeOpacity="0.4"
                  fill="none"
                  className="hero-dash"
                />
                <circle r="2.5" fill="var(--green)" opacity="0.85">
                  <animateMotion
                    dur={`${2.6 + i * 0.35}s`}
                    repeatCount="indefinite"
                    path={d}
                  />
                </circle>
              </g>
            )
          })}
        </g>

        {/* ── Provider source nodes (left) ────────────────────────────────── */}
        <g>
          {PROVIDERS.map((p, i) => {
            const y = providerY(i)
            return (
              <g key={p.label}>
                <rect
                  x={P_X}
                  y={y}
                  width={P_W}
                  height={P_H}
                  rx="3"
                  fill="var(--greenLt)"
                  stroke="var(--green)"
                  strokeWidth="1"
                  strokeOpacity="0.35"
                />
                {/* Provider name */}
                <text
                  x={P_X + 12}
                  y={y + P_H / 2 - 2}
                  fontSize="11"
                  fontWeight="600"
                  fontFamily="var(--font-geist-sans), sans-serif"
                  fill="var(--ink)"
                  dominantBaseline="middle"
                >
                  {p.label}
                </text>
                {/* Frequency badge */}
                <text
                  x={P_X + 12}
                  y={y + P_H / 2 + 12}
                  fontSize="8.5"
                  fontFamily="var(--font-geist-mono), monospace"
                  fill="var(--green)"
                  dominantBaseline="middle"
                  opacity="0.8"
                >
                  {p.freq === "daily" ? "daily" : `every ${p.freq}`}
                </text>
              </g>
            )
          })}
        </g>

        {/* ── Reconciliation engine (center) ──────────────────────────────── */}
        <g>
          {/* Breathing glow behind the box */}
          <rect
            x={R_X - 4}
            y={R_Y - 4}
            width={R_W + 8}
            height={R_H + 8}
            rx="6"
            fill="var(--accentLt)"
            className="hero-breathe"
          />
          {/* Main box */}
          <rect
            x={R_X}
            y={R_Y}
            width={R_W}
            height={R_H}
            rx="4"
            fill="var(--surfaceHi)"
            stroke="var(--border)"
            strokeWidth="1"
          />

          {/* Title */}
          <text
            x={R_X + R_W / 2}
            y={R_Y + 26}
            fontSize="9"
            fontWeight="700"
            fontFamily="var(--font-geist-mono), monospace"
            fill="var(--dim)"
            textAnchor="middle"
            letterSpacing="1.5"
          >
            RECONCILE
          </text>

          {/* Divider */}
          <line
            x1={R_X + 16}
            y1={R_Y + 38}
            x2={R_X + R_W - 16}
            y2={R_Y + 38}
            stroke="var(--border)"
            strokeWidth="0.5"
          />

          {/* Rule items */}
          {[
            "2-source agreement",
            ">5% → flagged",
            "Immutable records",
          ].map((rule, i) => (
            <g key={rule}>
              <text
                x={R_X + 28}
                y={R_Y + 60 + i * 22}
                fontSize="10"
                fontFamily="var(--font-geist-sans), sans-serif"
                fill="var(--muted)"
                dominantBaseline="middle"
              >
                {rule}
              </text>
              <circle
                cx={R_X + 18}
                cy={R_Y + 59 + i * 22}
                r="3.5"
                fill="var(--green)"
                opacity="0.7"
              />
              <text
                x={R_X + 18}
                y={R_Y + 60 + i * 22}
                fontSize="6"
                fontWeight="700"
                fill="var(--surfaceHi)"
                textAnchor="middle"
                dominantBaseline="middle"
              >
                ✓
              </text>
            </g>
          ))}

          {/* Divider before sparkline */}
          <line
            x1={R_X + 16}
            y1={R_Y + 130}
            x2={R_X + R_W - 16}
            y2={R_Y + 130}
            stroke="var(--border)"
            strokeWidth="0.5"
          />

          {/* Mini sparkline section */}
          <text
            x={R_X + 18}
            y={R_Y + 148}
            fontSize="8"
            fontFamily="var(--font-geist-mono), monospace"
            fill="var(--dim)"
            letterSpacing="0.5"
          >
            PRICE HISTORY
          </text>
          <g transform={`translate(${R_X + 18}, ${R_Y + 158})`}>
            {/* Sparkline fill area */}
            <path
              d={`${sparkline()} L ${SPARK_W},${SPARK_H} L 0,${SPARK_H} Z`}
              fill="url(#spark-grad)"
            />
            {/* Sparkline line */}
            <path
              d={sparkline()}
              stroke="var(--accent)"
              strokeWidth="1.5"
              fill="none"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {/* Current value dot */}
            <circle
              cx={SPARK_W}
              cy={SPARK_H - (SPARK_POINTS[SPARK_POINTS.length - 1] / Math.max(...SPARK_POINTS)) * SPARK_H}
              r="3"
              fill="var(--accent)"
            >
              <animate
                attributeName="r"
                values="2.5;4;2.5"
                dur="2s"
                repeatCount="indefinite"
              />
              <animate
                attributeName="opacity"
                values="1;0.6;1"
                dur="2s"
                repeatCount="indefinite"
              />
            </circle>
          </g>

          {/* Mock data labels */}
          <g transform={`translate(${R_X + 18}, ${R_Y + 198})`}>
            <rect
              width="50"
              height="16"
              rx="2"
              fill="var(--accentLt)"
              stroke="var(--accent)"
              strokeWidth="0.5"
              strokeOpacity="0.3"
            />
            <text
              x="25"
              y="11"
              fontSize="8"
              fontWeight="600"
              fontFamily="var(--font-geist-mono), monospace"
              fill="var(--accent)"
              textAnchor="middle"
            >
              $2.50/1M
            </text>
            <rect
              x="56"
              width="56"
              height="16"
              rx="2"
              fill="var(--greenLt)"
              stroke="var(--green)"
              strokeWidth="0.5"
              strokeOpacity="0.3"
            />
            <text
              x="84"
              y="11"
              fontSize="8"
              fontWeight="600"
              fontFamily="var(--font-geist-mono), monospace"
              fill="var(--green)"
              textAnchor="middle"
            >
              HIGH conf.
            </text>
          </g>

          {/* Status line */}
          <g transform={`translate(${R_X + 18}, ${R_Y + 226})`}>
            <circle cx="4" cy="6" r="3" fill="var(--green)" opacity="0.8">
              <animate
                attributeName="opacity"
                values="0.5;1;0.5"
                dur="2.5s"
                repeatCount="indefinite"
              />
            </circle>
            <text
              x="12"
              y="10"
              fontSize="9"
              fontFamily="var(--font-geist-sans), sans-serif"
              fill="var(--green)"
            >
              3 sources verified
            </text>
          </g>
        </g>

        {/* ── API endpoint nodes (right) ──────────────────────────────────── */}
        <g>
          {ENDPOINTS.map((ep, i) => {
            const y = endpointY(i)
            return (
              <g key={ep.path}>
                <rect
                  x={E_X}
                  y={y}
                  width={E_W}
                  height={E_H}
                  rx="3"
                  fill="var(--surfaceHi)"
                  stroke="var(--border)"
                  strokeWidth="1"
                />
                {/* Endpoint path */}
                <text
                  x={E_X + 10}
                  y={y + E_H / 2 - 4}
                  fontSize="10"
                  fontWeight="600"
                  fontFamily="var(--font-geist-mono), monospace"
                  fill="var(--ink)"
                  dominantBaseline="middle"
                >
                  {ep.path}
                </text>
                {/* Endpoint label */}
                <text
                  x={E_X + 10}
                  y={y + E_H / 2 + 10}
                  fontSize="8"
                  fontWeight="600"
                  fontFamily="var(--font-geist-mono), monospace"
                  fill="var(--green)"
                  dominantBaseline="middle"
                  opacity="0.8"
                >
                  endpoint
                </text>
              </g>
            )
          })}
        </g>

        {/* ── Column labels ────────────────────────────────────────────────── */}
        <text
          x={P_X + P_W / 2}
          y={12}
          fontSize="8"
          fontWeight="600"
          fontFamily="var(--font-geist-mono), monospace"
          fill="var(--dim)"
          textAnchor="middle"
          letterSpacing="1"
        >
          SOURCES
        </text>
        <text
          x={E_X + E_W / 2}
          y={12}
          fontSize="8"
          fontWeight="600"
          fontFamily="var(--font-geist-mono), monospace"
          fill="var(--dim)"
          textAnchor="middle"
          letterSpacing="1"
        >
          API
        </text>

        {/* ── Latency badge (bottom right) ─────────────────────────────────── */}
        <g transform={`translate(${E_X + 8}, ${E_Y0 + 4 * E_GAP - 14})`}>
          <rect
            width="68"
            height="16"
            rx="2"
            fill="var(--surfaceLo)"
            stroke="var(--border)"
            strokeWidth="0.5"
          />
          <text
            x="34"
            y="11"
            fontSize="8"
            fontFamily="var(--font-geist-mono), monospace"
            fill="var(--muted)"
            textAnchor="middle"
          >
            p99 &lt;200ms
          </text>
        </g>
      </svg>
    </div>
  )
}
