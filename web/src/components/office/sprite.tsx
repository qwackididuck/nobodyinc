// The agents' pixel characters, drawn from a 10×12 map of their look
// (lib/look.ts): H hair, S skin, E eyes, M mouth, C shirt, T tie, D trousers.

import type { Look } from "@/lib/look";

const MAP = [
  "...HHHH...",
  "..HHHHHH..",
  ".HHSSSSHH.",
  ".HSESSESH.",
  "..SSSSSS..",
  "..SSMMSS..",
  "...SSSS...",
  ".CCCTTCCC.",
  "CCCCTTCCCC",
  "SCCCTTCCCS",
  "SCCCCCCCCS",
  ".DDD..DDD.",
];

interface Pixel {
  x: number;
  y: number;
  fill: string;
}

function pixels(look: Look): Pixel[] {
  const fill: Record<string, string> = { H: look.hair, S: look.skin, E: "#1d1726", M: "#a5544c", C: look.shirt, T: look.tie, D: "#3a3550" };
  const out: Pixel[] = [];
  MAP.forEach((row, y) => {
    [...row].forEach((k, x) => {
      if (k !== ".") out.push({ x, y, fill: fill[k] });
    });
  });
  return out;
}

// Sprite renders a character in React.
export function Sprite({ look, className }: { look: Look; className?: string }) {
  return (
    <svg className={className} viewBox="0 0 10 12" shapeRendering="crispEdges" aria-hidden="true">
      {pixels(look).map((p) => (
        <rect key={`${p.x}-${p.y}`} x={p.x} y={p.y} width="1" height="1" fill={p.fill} />
      ))}
    </svg>
  );
}

// spriteSVG returns the same character as markup, for elements created
// outside React (the walkers of the office animations).
export function spriteSVG(look: Look): string {
  const rects = pixels(look)
    .map((p) => `<rect x="${p.x}" y="${p.y}" width="1" height="1" fill="${p.fill}"/>`)
    .join("");
  return `<svg viewBox="0 0 10 12" shape-rendering="crispEdges" aria-hidden="true">${rects}</svg>`;
}

const PLANE_MAP = [
  "KK..........",
  "KWKK........",
  ".KWWWKK.....",
  "..KWWWWWKKKK",
  "..KWGGGGKK..",
  ".KGGKK......",
  "KGKK........",
  "KK..........",
];

// PLANE_SVG is the paper plane, pointing right.
export const PLANE_SVG = (() => {
  const fill: Record<string, string> = { K: "#1d1726", W: "#fffaf0", G: "#bdb3d1" };
  let rects = "";
  PLANE_MAP.forEach((row, y) => {
    [...row].forEach((k, x) => {
      if (k !== ".") rects += `<rect x="${x}" y="${y}" width="1" height="1" fill="${fill[k]}"/>`;
    });
  });
  return `<svg viewBox="0 0 12 8" shape-rendering="crispEdges">${rects}</svg>`;
})();
