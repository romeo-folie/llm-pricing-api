"use client";

import { useEffect, useRef } from "react";
import styles from "./WireframeCube.module.css";

interface WireframeCubeProps {
  /** Center X in parent SVG viewBox coords. */
  cx?: number;
  /** Center Y in parent SVG viewBox coords. */
  cy?: number;
  /** Half-size of cube in SVG units. Default 38. */
  size?: number;
  /** Rotation speed (radians per frame). Default 0.008. */
  speed?: number;
  /** X-axis tilt in radians. Default -0.35 (~-20deg). */
  tiltX?: number;
}

const VERTS: [number, number, number][] = [
  [-1, -1, -1],
  [1, -1, -1],
  [1, 1, -1],
  [-1, 1, -1],
  [-1, -1, 1],
  [1, -1, 1],
  [1, 1, 1],
  [-1, 1, 1],
];

const EDGES: [number, number][] = [
  [0, 1], [1, 2], [2, 3], [3, 0],
  [4, 5], [5, 6], [6, 7], [7, 4],
  [0, 4], [1, 5], [2, 6], [3, 7],
];

const FACES: number[][] = [
  [0, 1, 2, 3],
  [4, 5, 6, 7],
  [0, 1, 5, 4],
  [2, 3, 7, 6],
  [0, 3, 7, 4],
  [1, 2, 6, 5],
];

/**
 * Pure SVG wireframe cube with real 3D projection.
 *
 * Renders a <g> — place inside your pipeline <svg>.
 * Uses requestAnimationFrame for smooth rotation.
 */
export function WireframeCube({
  cx = 380,
  cy = 170,
  size = 38,
  speed = 0.008,
  tiltX = -0.35,
}: WireframeCubeProps) {
  const groupRef = useRef<SVGGElement>(null);
  const angleRef = useRef(0);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    const g = groupRef.current;
    if (!g) return;

    // Respect prefers-reduced-motion: skip animation entirely.
    const reducedMotion =
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    const ns = "http://www.w3.org/2000/svg";
    const d = 7.5; // perspective distance — matches CSS perspective:300px on 80px cube

    // Create face polygons (inserted first so edges draw on top)
    const faceEls: SVGPolygonElement[] = [];
    for (let i = 0; i < FACES.length; i++) {
      const p = document.createElementNS(ns, "polygon");
      p.setAttribute("class", styles.face);
      g.appendChild(p);
      faceEls.push(p);
    }

    // Create edge lines
    const lineEls: SVGLineElement[] = [];
    for (let i = 0; i < EDGES.length; i++) {
      const l = document.createElementNS(ns, "line");
      l.setAttribute("class", styles.edge);
      g.appendChild(l);
      lineEls.push(l);
    }

    // Create core circle
    const core = document.createElementNS(ns, "circle");
    core.setAttribute("cx", String(cx));
    core.setAttribute("cy", String(cy));
    core.setAttribute("r", "5");
    core.setAttribute("class", styles.core);
    g.appendChild(core);

    // Create glow ring
    const glow = document.createElementNS(ns, "circle");
    glow.setAttribute("cx", String(cx));
    glow.setAttribute("cy", String(cy));
    glow.setAttribute("r", "10");
    glow.setAttribute("class", styles.glow);
    g.appendChild(glow);

    function rotY(v: number[], a: number): number[] {
      const c = Math.cos(a), s = Math.sin(a);
      return [v[0] * c + v[2] * s, v[1], -v[0] * s + v[2] * c];
    }

    function rotX(v: number[], a: number): number[] {
      const c = Math.cos(a), s = Math.sin(a);
      return [v[0], v[1] * c - v[2] * s, v[1] * s + v[2] * c];
    }

    function project(v: number[]): [number, number] {
      const scale = d / (d + v[2]);
      return [cx + v[0] * size * scale, cy + v[1] * size * scale];
    }

    // draw() only updates geometry — no scheduling side-effects.
    function draw() {
      angleRef.current += speed;
      const projected: [number, number][] = [];

      for (let i = 0; i < VERTS.length; i++) {
        const r = rotX(rotY([...VERTS[i]], angleRef.current), tiltX);
        projected.push(project(r));
      }

      for (let i = 0; i < EDGES.length; i++) {
        const a = projected[EDGES[i][0]];
        const b = projected[EDGES[i][1]];
        lineEls[i].setAttribute("x1", String(a[0]));
        lineEls[i].setAttribute("y1", String(a[1]));
        lineEls[i].setAttribute("x2", String(b[0]));
        lineEls[i].setAttribute("y2", String(b[1]));
      }

      for (let i = 0; i < FACES.length; i++) {
        const pts = FACES[i]
          .map((idx) => `${projected[idx][0]},${projected[idx][1]}`)
          .join(" ");
        faceEls[i].setAttribute("points", pts);
      }
    }

    // frame() = draw + re-schedule (animation loop only; never called for static render).
    function frame() {
      draw();
      rafRef.current = requestAnimationFrame(frame);
    }

    // Render one static frame so the cube is visible immediately.
    // For reduced motion we stop here — no RAF loop is started.
    draw();

    if (!reducedMotion) {
      rafRef.current = requestAnimationFrame(frame);
    }

    return () => {
      cancelAnimationFrame(rafRef.current);
      // Remove all imperatively created child nodes so effect re-runs
      // (React 18 StrictMode double-invoke, prop changes) don't accumulate
      // duplicate polygons/lines/circles in the DOM.
      while (g.firstChild) {
        g.removeChild(g.firstChild);
      }
    };
  }, [cx, cy, size, speed, tiltX]);

  return <g ref={groupRef} />;
}
