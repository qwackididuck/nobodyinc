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

// popText shows a short text (what an agent reads at the library, "+2
// PAGES") at a point for duration milliseconds. The text is set as text,
// never as HTML: it may come from an agent.
export function popText(layer: HTMLElement, className: string, text: string, at: Point, duration: number): void {
  const el = add(layer, className, at);
  el.textContent = text;
  setTimeout(() => el.remove(), duration);
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

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

// Construction words, picked at random over a building site.
const SITE_WORDS = ["TAP", "BANG!", "BZZZ", "KLONK", "TAP TAP", "CRASH!", "VRRR", "BOOM"];

// site plays a building site over a room: clouds of smoke rise all over it,
// dust flies, and onomatopoeias pop up above the smoke. It lasts about
// duration milliseconds.
export function site(layer: HTMLElement, box: Box, duration: number, moving: boolean): void {
  if (!moving) return;
  const rand = (a: number, b: number) => a + Math.random() * (b - a);
  const area = (box.width * box.height) / 1000;

  // Smoke: grey pixel clouds that swell, drift up and fade.
  const puffs = Math.round(Math.min(64, 18 + area / 2));
  for (let i = 0; i < puffs; i++) {
    const puff = add(layer, styles.smoke, { x: box.x + rand(0, box.width - 16), y: box.y + rand(box.height * 0.2, box.height - 16) });
    const delay = rand(0, duration * 0.7);
    puff.animate(
      [
        { transform: "translate(0, 0) scale(0.4)", opacity: 0 },
        { transform: `translate(${rand(-6, 6)}px, -10px) scale(1)`, opacity: 0.95, offset: 0.25 },
        { transform: `translate(${rand(-16, 16)}px, ${-rand(30, 60)}px) scale(${rand(1.8, 3)})`, opacity: 0 },
      ],
      { duration: rand(900, 1500), delay, easing: "steps(8)", fill: "both" },
    ).onfinish = () => puff.remove();
  }

  // Dust: small chips thrown around.
  for (let i = 0; i < Math.round(Math.min(36, 12 + area / 6)); i++) {
    const from = { x: box.x + rand(10, box.width - 10), y: box.y + rand(box.height * 0.4, box.height - 10) };
    const dust = add(layer, styles.dust, from);
    const angle = rand(0, Math.PI * 2);
    const r = rand(20, 60);
    dust.animate(
      [{ transform: "translate(0, 0) scale(1)" }, { transform: `translate(${Math.cos(angle) * r}px, ${Math.sin(angle) * r * 0.6}px) scale(0.3)`, opacity: 0 }],
      { duration: 900, delay: rand(0, duration * 0.8), easing: "steps(6)", fill: "both" },
    ).onfinish = () => dust.remove();
  }

  // Words, above the smoke.
  const words = Math.round(Math.min(9, 4 + area / 15));
  for (let i = 0; i < words; i++) {
    const delay = (i / words) * duration * 0.8;
    const word = add(layer, `${styles.worksWord} ${styles.siteWord}`, { x: box.x + rand(6, box.width - 70), y: box.y + rand(20, box.height - 30) }, SITE_WORDS[i % SITE_WORDS.length]);
    word.style.animationDelay = `${delay}ms`;
    word.style.transform = `rotate(${Math.round(rand(-2, 2)) * 4}deg)`;
    setTimeout(() => word.remove(), delay + 1400);
  }
}

// dismantle shows a room being taken down where it stood: its walls come
// down behind a building site.
export function dismantle(layer: HTMLElement, box: Box, moving: boolean): void {
  if (!moving) return;
  const ghost = add(layer, styles.dismantled, { x: box.x, y: box.y });
  ghost.style.width = `${box.width}px`;
  ghost.style.height = `${box.height}px`;
  ghost.animate([{ clipPath: "inset(0)" }, { clipPath: "inset(0 0 100% 0)" }], { duration: 1600, easing: "steps(10)", fill: "forwards" }).onfinish = () =>
    ghost.remove();
  site(layer, box, 1600, moving);
}

// fileTickets carries a finished feature's tickets from the director's board
// to the archives' shelves: a pile of cards slides there while a few loose
// cards flutter after it. It resolves once they are filed.
export function fileTickets(layer: HTMLElement, from: Point, to: Point, moving: boolean): Promise<void> {
  if (!moving) return Promise.resolve();
  const travel = { transform: `translate(${to.x - from.x}px, ${to.y - from.y}px)` };
  const done: Promise<unknown>[] = [];

  const pile = add(layer, styles.pile, from);
  done.push(
    pile.animate([{ transform: "translate(0, 0)" }, { transform: `translate(${(to.x - from.x) / 2}px, ${(to.y - from.y) / 2 - 30}px)`, offset: 0.5 }, travel], {
      duration: 1400,
      easing: "steps(14)",
      fill: "forwards",
    }).finished,
  );

  for (let i = 0; i < 4; i++) {
    const card = add(layer, styles.card, { x: from.x + 6, y: from.y });
    done.push(
      card.animate(
        [
          { transform: "translate(0, 0) rotate(0deg)" },
          { transform: `translate(${(to.x - from.x) / 2 + (i - 1.5) * 18}px, ${(to.y - from.y) / 2 - 50}px) rotate(${(i - 1.5) * 30}deg)`, offset: 0.5 },
          { transform: `translate(${to.x - from.x}px, ${to.y - from.y}px) rotate(0deg)` },
        ],
        { duration: 1500, delay: 120 + i * 140, easing: "steps(15)", fill: "forwards" },
      ).finished.then(() => card.remove()),
    );
  }

  return Promise.all(done).then(() => pile.remove());
}
