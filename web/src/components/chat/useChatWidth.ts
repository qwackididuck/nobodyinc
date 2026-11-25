"use client";

// The chat panel's width: the human drags the panel's right edge, and the
// width is remembered in this browser.

import { useCallback, useState } from "react";

import { readStored, writeStored } from "@/lib/storage";

const KEY = "nobody.chatWidth";
export const CHAT_MIN = 320;
export const CHAT_DEFAULT = 380;

// clampWidth keeps the panel between its minimum and 70% of the window, so the
// main area always stays usable.
export function clampWidth(w: number): number {
  const max = Math.max(CHAT_MIN, Math.round(window.innerWidth * 0.7));
  return Math.min(max, Math.max(CHAT_MIN, Math.round(w)));
}

export function useChatWidth() {
  const [width, setWidthState] = useState(CHAT_DEFAULT);
  // Loaded after the first render: the page is prerendered, storage is browser-only.
  const [loaded, setLoaded] = useState(false);
  if (!loaded && typeof window !== "undefined") {
    setLoaded(true);
    setWidthState(clampWidth(readStored<number>(KEY, CHAT_DEFAULT)));
  }

  const setWidth = useCallback((w: number) => setWidthState(clampWidth(w)), []);
  const remember = useCallback((w: number) => writeStored(KEY, clampWidth(w)), []);
  return { width, setWidth, remember };
}
