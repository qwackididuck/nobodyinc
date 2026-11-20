"use client";

// How the office looks on this computer (Settings → Appearance), kept in
// localStorage. The operating system's "reduce motion" setting always wins
// over "full".

import { useMemo, useSyncExternalStore } from "react";

import { readStored, writeStored } from "./storage";

export type Animations = "full" | "reduced" | "off";

export interface Appearance {
  // full: walks, loops, confetti; reduced: statuses and straight paper
  // planes only; off: nothing moves (the speech bubbles of teammates still
  // show: they are a state, who waits for an answer).
  animations: Animations;
  // Show who writes to whom, and the subject, next to each paper plane (the
  // planes between teams: teammates' speech bubbles always hold the message).
  planeLabels: boolean;
}

const KEY = "nobody.appearance";
const CHANGED = "nobody:appearance";
const DEFAULTS: Appearance = { animations: "full", planeLabels: true };

export function saveAppearance(a: Appearance): void {
  writeStored(KEY, a);
  window.dispatchEvent(new Event(CHANGED));
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener(CHANGED, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(CHANGED, onChange);
    window.removeEventListener("storage", onChange);
  };
}

// snapshot is the stored value and the system's motion setting, as a string
// so that React can compare it.
function snapshot(): string {
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  return `${reduce ? "reduce" : "motion"}|${JSON.stringify(readStored<Partial<Appearance>>(KEY, {}))}`;
}

// useAppearance returns the appearance in force, updated when Settings (or
// another tab) changes it.
export function useAppearance(): Appearance {
  const raw = useSyncExternalStore(subscribe, snapshot, () => "motion|{}");

  return useMemo(() => {
    const [motion, stored] = [raw.slice(0, raw.indexOf("|")), raw.slice(raw.indexOf("|") + 1)];
    const a: Appearance = { ...DEFAULTS, ...(JSON.parse(stored) as Partial<Appearance>) };
    return motion === "reduce" && a.animations === "full" ? { ...a, animations: "reduced" } : a;
  }, [raw]);
}
