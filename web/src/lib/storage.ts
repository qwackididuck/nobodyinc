// Per-browser preferences and read marks, in localStorage. Storage can be
// missing or refuse access (private windows, blocked site data): every call
// falls back to the default and never throws.

export function readStored<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    return raw === null ? fallback : (JSON.parse(raw) as T);
  } catch {
    return fallback;
  }
}

export function writeStored(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Not remembered: the page keeps working.
  }
}
