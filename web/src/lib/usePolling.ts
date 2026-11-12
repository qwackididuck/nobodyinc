"use client";

import { useCallback, useEffect, useState } from "react";

// usePolling calls load every intervalMs and keeps the latest result. The
// daemon has no push channel for agents and messages yet; polling every couple
// of seconds is enough for a single operator.
export function usePolling<T>(load: () => Promise<T>, intervalMs: number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setData(await load());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [load]);

  useEffect(() => {
    // The first load starts right away; later ones follow the interval.
    const first = setTimeout(refresh, 0);
    const id = setInterval(refresh, intervalMs);
    return () => {
      clearTimeout(first);
      clearInterval(id);
    };
  }, [refresh, intervalMs]);

  return { data, error, refresh };
}
