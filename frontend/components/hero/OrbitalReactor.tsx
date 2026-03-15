"use client";

import styles from "./OrbitalReactor.module.css";

interface OrbitalReactorProps {
  cx?: number;
  cy?: number;
  scale?: number;
}

/**
 * Pure SVG orbital reactor — drop inside any <svg> as a <g> element.
 * Colors are wired to the project's CSS design tokens.
 */
export function OrbitalReactor({ cx = 340, cy = 170, scale = 1 }: OrbitalReactorProps) {
  return (
    <g
      transform={
        scale !== 1
          ? `translate(${cx},${cy}) scale(${scale}) translate(${-cx},${-cy})`
          : undefined
      }
    >
      {/* Ring 1 — tilted 25°, fast */}
      <g className={styles.ring1} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <ellipse
          cx={cx} cy={cy} rx={44} ry={16}
          fill="none"
          stroke="var(--accent)"
          strokeWidth={0.75}
          opacity={0.3}
          transform={`rotate(25,${cx},${cy})`}
        />
      </g>

      {/* Ring 2 — tilted -35°, medium */}
      <g className={styles.ring2} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <ellipse
          cx={cx} cy={cy} rx={56} ry={14}
          fill="none"
          stroke="var(--accent)"
          strokeWidth={0.5}
          opacity={0.18}
          transform={`rotate(-35,${cx},${cy})`}
        />
      </g>

      {/* Ring 3 — flat, slow */}
      <g className={styles.ring3} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <ellipse
          cx={cx} cy={cy} rx={52} ry={18}
          fill="none"
          stroke="var(--accent)"
          strokeWidth={0.5}
          opacity={0.22}
        />
      </g>

      {/* Data particles — bright green to match reactor "sun" */}
      <g className={styles.dot1Wrap} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <circle cx={cx} cy={cy - 16} r={3.5} fill="var(--greenBright)" opacity="0.7" className={styles.dot} />
      </g>
      <g className={styles.dot2Wrap} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <circle cx={cx + 56} cy={cy} r={3} fill="var(--greenBright)" opacity="0.8" className={styles.dot} />
      </g>
      <g className={styles.dot3Wrap} style={{ transformOrigin: `${cx}px ${cy}px` }}>
        <circle cx={cx - 52} cy={cy} r={3} fill="var(--greenBright)" opacity="0.6" className={styles.dot} />
      </g>

      {/* Core — bright green "sun" */}
      <circle cx={cx} cy={cy} r={12} fill="var(--greenBright)" className={styles.core} />

      {/* Outer echo ring — green glow halo around the sun */}
      <circle
        cx={cx} cy={cy} r={18}
        fill="none"
        stroke="var(--greenBright)"
        strokeWidth={0.5}
        opacity={0.25}
        className={styles.coreEcho}
      />
    </g>
  );
}
