// The office's passing animations: paper planes, walkers, construction work
// and celebrations. They are short-lived elements added to a layer above the
// scene and removed when done, rather than React state: dozens of particles
// would otherwise re-render the office many times per second.

import type { Look } from "@/lib/look";
import { flightPath } from "./flight";
import type { Point } from "./layout";
import styles from "./office.module.css";
import { PLANE_SVG, spriteSVG } from "./sprite";

// Walking pace, in milliseconds per pixel.
const WALK_MS_PER_PX = 6;
// Plane speed in pixels per millisecond, before the slope changes it.
const PLANE_SPEED = 0.3;

function add(layer: HTMLElement, className: string, at: Point, html = ""): HTMLElement {
  const el = document.createElement("div");
  el.className = className;
  el.style.left = `${at.x}px`;
  el.style.top = `${at.y}px`;
  el.innerHTML = html;
  layer.append(el);
  return el;
}

const snap = (v: number) => `${Math.round(v / 2) * 2}px`;

interface FlightOptions {
  natural: boolean; // loops and wobbles; false: a straight line
  label: string | null;
  reply: boolean; // the label is drawn as a reply
}

// flyPlane sends a paper plane from one point to another and resolves when
// it lands. It slows down when climbing and speeds up when diving, and
// points where it flies, in steps of 15°.
export function flyPlane(layer: HTMLElement, from: Point, to: Point, opts: FlightOptions): Promise<void> {
  const pts = flightPath(from, to, opts.natural);
  const plane = add(layer, styles.plane, from, PLANE_SVG);
  const top = pts.reduce((m, p) => (p.y < m.y ? p : m), pts[0]);
  const label = opts.label ? add(layer, `${styles.planeLabel} ${opts.reply ? styles.planeReply : ""}`, { x: top.x, y: top.y - 6 }) : null;
  if (label && opts.label) label.textContent = opts.label;

  const segLen = (k: number) => Math.hypot(pts[k + 1].x - pts[k].x, pts[k + 1].y - pts[k].y) || 0.001;

  return new Promise((done) => {
    let i = 0;
    let along = 0;
    let last = performance.now();
    let frame = 0;

    function step(now: number) {
      const dt = Math.min(48, now - last);
      last = now;
      if (i < pts.length - 1) along += PLANE_SPEED * (1 + 0.7 * ((pts[i + 1].y - pts[i].y) / segLen(i))) * dt;
      while (i < pts.length - 1 && along >= segLen(i)) {
        along -= segLen(i);
        i += 1;
      }

      const a = pts[Math.min(i, pts.length - 1)];
      const b = pts[Math.min(i + 1, pts.length - 1)];
      const f = Math.min(1, along / (Math.hypot(b.x - a.x, b.y - a.y) || 1));
      const x = a.x + (b.x - a.x) * f;
      const y = a.y + (b.y - a.y) * f;
      plane.style.left = snap(x);
      plane.style.top = snap(y);
      const deg = Math.round((Math.atan2(b.y - a.y, b.x - a.x) * 180) / Math.PI / 15) * 15;
      plane.style.transform = `rotate(${deg}deg)`;

      if (opts.natural && frame++ % 3 === 0) {
        const trail = add(layer, styles.trail, { x, y });
        setTimeout(() => trail.remove(), 800);
      }

      if (i < pts.length - 1) {
        requestAnimationFrame(step);
        return;
      }
      plane.remove();
      setTimeout(() => label?.remove(), 900);
      done();
    }

    requestAnimationFrame(step);
  });
}

// addWalker puts a character on its feet at a station position.
export function addWalker(layer: HTMLElement, look: Look, at: Point): HTMLElement {
  return add(layer, `${styles.walker} ${styles.walking}`, at, `<span class="${styles.char}">${spriteSVG(look)}</span>`);
}

// walk moves a walker along station positions (the first one is where it
// was added) at a steady pace, and resolves when it arrives.
export function walk(walker: HTMLElement, path: Point[], moving: boolean): Promise<void> {
  const origin = path[0];
  const lengths = path.slice(1).map((p, i) => Math.hypot(p.x - path[i].x, p.y - path[i].y));
  const total = lengths.reduce((s, l) => s + l, 0) || 1;
  let acc = 0;
  const frames = path.map((p, i) => {
    if (i > 0) acc += lengths[i - 1];
    return { transform: `translate(${p.x - origin.x}px, ${p.y - origin.y}px)`, offset: acc / total };
  });

  return new Promise((done) => {
    walker.classList.add(styles.walking);
    const animation = walker.animate(frames, { duration: moving ? total * WALK_MS_PER_PX : 1, easing: "linear", fill: "forwards" });
    animation.onfinish = () => {
      walker.classList.remove(styles.walking);
      done();
    };
  });
}

// works plays a quick construction around a desk: dust and "tap tap".
export function works(layer: HTMLElement, desk: Point, moving: boolean): void {
  if (!moving) return;
  ["TAP", "BANG!", "TAP"].forEach((word, i) => {
    const el = add(layer, styles.worksWord, { x: desk.x + 10 + i * 34, y: desk.y + 20 - (i % 2) * 18 }, word);
    el.style.animationDelay = `${i * 220}ms`;
    setTimeout(() => el.remove(), 1400);
  });
  for (let i = 0; i < 16; i++) {
    const dust = add(layer, styles.dust, { x: desk.x + 52, y: desk.y + 64 });
    const angle = Math.random() * Math.PI * 2;
    const r = 30 + Math.random() * 50;
    dust.animate(
      [{ transform: "translate(0, 0) scale(1)" }, { transform: `translate(${Math.cos(angle) * r}px, ${Math.sin(angle) * r * 0.6}px) scale(0.3)`, opacity: 0 }],
      { duration: 900, delay: Math.random() * 300, easing: "steps(6)", fill: "forwards" },
    ).onfinish = () => dust.remove();
  }
}

// celebrate shows a pixel toast, with confetti when things move.
export function celebrate(layer: HTMLElement, title: string, text: string, at: Point, moving: boolean): void {
  const toast = add(layer, styles.toast, at, `<strong></strong><span></span>`);
  (toast.querySelector("strong") as HTMLElement).textContent = title;
  (toast.querySelector("span") as HTMLElement).textContent = text;
  setTimeout(() => toast.remove(), 4100);
  if (!moving) return;

  const colors = ["#ffd34d", "#ff8a5c", "#6fdc8c", "#3fb0e0", "#ef6b86"];
  for (let i = 0; i < 26; i++) {
    const c = add(layer, styles.confetti, { x: at.x, y: at.y + 30 });
    c.style.background = colors[i % colors.length];
    const angle = Math.random() * Math.PI * 2;
    const d = 80 + Math.random() * 120;
    c.animate([{ transform: "translate(0, 0)" }, { transform: `translate(${Math.cos(angle) * d}px, ${Math.sin(angle) * d + 80}px)`, opacity: 0 }], {
      duration: 1400,
      easing: "steps(10)",
    }).onfinish = () => c.remove();
  }
}

// dismantle shows a room being taken down where it stood.
export function dismantle(layer: HTMLElement, box: { x: number; y: number; width: number; height: number }, moving: boolean): void {
  if (!moving) return;
  const ghost = add(layer, styles.dismantled, { x: box.x, y: box.y });
  ghost.style.width = `${box.width}px`;
  ghost.style.height = `${box.height}px`;
  ghost.animate([{ clipPath: "inset(0)" }, { clipPath: "inset(0 0 100% 0)" }], { duration: 1200, easing: "steps(10)", fill: "forwards" }).onfinish = () =>
    ghost.remove();
  works(layer, { x: box.x + box.width / 2 - 52, y: box.y + box.height / 2 - 60 }, moving);
}
