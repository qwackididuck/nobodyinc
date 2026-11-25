"use client";

// Hidden history: a chat opens on what happened since the window was opened
// or since the last feature was finished; older messages stay behind a
// "show earlier" button. The cutoff lives in memory only, so a new window
// starts clean.

import { useEffect, useRef, useState } from "react";

import { useLive } from "@/lib/live";

export function useHistoryCutoff(): string {
  const { features, loaded } = useLive();
  const [cutoff, setCutoff] = useState(() => new Date().toISOString());
  const finished = useRef<Set<string> | null>(null);

  useEffect(() => {
    if (!loaded.features) return;
    const now = new Set(features.filter((f) => f.status === "finished").map((f) => f.id));
    const before = finished.current;
    finished.current = now;
    // The first load only records what was already finished.
    if (before && [...now].some((id) => !before.has(id))) setCutoff(new Date().toISOString());
  }, [features, loaded.features]);

  return cutoff;
}
