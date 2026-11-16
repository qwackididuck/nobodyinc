// The flight path of a paper plane, as a polyline of points.
//
// A natural flight is a cubic curve with random climbs and dips, a gentle
// wobble, and on long flights, sometimes a loop-the-loop that always starts
// upwards. A straight flight (reduced animations) is just the line between
// the two desks.

import type { Point } from "./layout";

const SAMPLES = 140;
const LOOP_SAMPLES = 48;

export function flightPath(from: Point, to: Point, natural: boolean, random: () => number = Math.random): Point[] {
  if (!natural) return [from, to];

  const rand = (a: number, b: number) => a + random() * (b - a);
  const dist = Math.hypot(to.x - from.x, to.y - from.y) || 1;
  const n = { x: -(to.y - from.y) / dist, y: (to.x - from.x) / dist }; // normal to the straight line
  const c1 = {
    x: from.x + (to.x - from.x) * 0.3 + n.x * rand(-0.45, 0.45) * dist,
    y: from.y + (to.y - from.y) * 0.3 + n.y * rand(-0.45, 0.45) * dist - rand(40, 130),
  };
  const c2 = {
    x: from.x + (to.x - from.x) * 0.7 + n.x * rand(-0.45, 0.45) * dist,
    y: from.y + (to.y - from.y) * 0.7 + n.y * rand(-0.45, 0.45) * dist - rand(0, 90),
  };
  const wobble = { a: rand(5, 16), f: rand(1.5, 3.5), phase: rand(0, Math.PI * 2) };

  const pts: Point[] = [];
  for (let i = 0; i <= SAMPLES; i++) {
    const t = i / SAMPLES;
    const u = 1 - t;
    const w = wobble.a * Math.sin(2 * Math.PI * wobble.f * t + wobble.phase) * Math.sin(Math.PI * t);
    pts.push({
      x: u * u * u * from.x + 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * to.x + n.x * w,
      y: u * u * u * from.y + 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * to.y + n.y * w,
    });
  }

  if (dist > 160 && random() < 0.6) insertLoop(pts, Math.round(SAMPLES * rand(0.35, 0.6)), rand(22, 36));

  return pts;
}

// insertLoop adds a loop of radius r after point k, going up first.
function insertLoop(pts: Point[], k: number, r: number): void {
  const a = pts[k - 1];
  const b = pts[k];
  const len = Math.hypot(b.x - a.x, b.y - a.y) || 1;
  const t = { x: (b.x - a.x) / len, y: (b.y - a.y) / len }; // direction of flight
  let up = { x: -t.y, y: t.x };
  if (up.y > 0) up = { x: -up.x, y: -up.y }; // the normal that points up the screen

  const loop: Point[] = [];
  for (let i = 1; i <= LOOP_SAMPLES; i++) {
    const th = (i / LOOP_SAMPLES) * Math.PI * 2;
    loop.push({
      x: b.x + t.x * r * Math.sin(th) + up.x * r * (1 - Math.cos(th)),
      y: b.y + t.y * r * Math.sin(th) + up.y * r * (1 - Math.cos(th)),
    });
  }
  pts.splice(k + 1, 0, ...loop);
}
