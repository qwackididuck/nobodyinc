"use client";

import { useSyncExternalStore } from "react";

import { checkUpdate, getUpdate, type UpdateStatus } from "./launcher";
import { readStored, writeStored } from "./storage";

// The update check's state, shared by the notice and the menus that start a
// check (docs/updates.md). One store per page: the launcher keeps the result.

// What the human last asked for and learned, shown once by the notice.
export type UpdateOutcome = { kind: "uptodate" | "dev"; current: string } | { kind: "error"; message: string };

export interface UpdateState {
  status: UpdateStatus | null;
  checking: boolean;
  outcome: UpdateOutcome | null;
  // The release the human chose to be reminded of later ("Later").
  dismissed: string | null;
}

const DISMISSED_KEY = "update-dismissed";

let state: UpdateState = { status: null, checking: false, outcome: null, dismissed: null };
const listeners = new Set<() => void>();

function set(next: Partial<UpdateState>) {
  state = { ...state, ...next };
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useUpdate(): UpdateState {
  return useSyncExternalStore(subscribe, () => state, () => state);
}

// A release version is vX.Y.Z; a development build is never compared.
const releaseVersion = /^v\d+\.\d+\.\d+$/;

// How long the start-up check may take before the page stops waiting for it:
// the launcher runs it in the background, so a page opened right away may
// ask before it ended.
const POLL_MS = 3_000;
const POLL_TRIES = 5;

// watchUpdate reads the start-up check's result, waiting a little for it.
// It returns a function that stops waiting.
export function watchUpdate(): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  set({ dismissed: readStored<string | null>(DISMISSED_KEY, null) });

  const poll = async (left: number) => {
    try {
      const status = await getUpdate();
      if (stopped) return;
      set({ status });
      if (status.checked_at || left <= 1) return;
    } catch {
      // The launcher cannot be reached: the projects page says so.
      return;
    }
    timer = setTimeout(() => void poll(left - 1), POLL_MS);
  };

  void poll(POLL_TRIES);

  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}

// checkNow is "Check for updates": it asks GitHub and tells the human the
// result, which may be a reason to show the banner again.
export async function checkNow(): Promise<void> {
  if (state.checking) return;
  set({ checking: true, outcome: null });
  try {
    const status = await checkUpdate();
    if (status.error) {
      set({ status, outcome: { kind: "error", message: status.error } });
    } else if (status.available) {
      // Asked on purpose: show the banner even if "Later" was chosen before.
      writeStored(DISMISSED_KEY, null);
      set({ status, dismissed: null });
    } else {
      set({ status, outcome: { kind: releaseVersion.test(status.current) ? "uptodate" : "dev", current: status.current } });
    }
  } catch (e) {
    set({ outcome: { kind: "error", message: (e as Error).message } });
  } finally {
    set({ checking: false });
  }
}

// dismissRelease hides the banner for this release; a newer one shows it again.
export function dismissRelease(tag: string) {
  writeStored(DISMISSED_KEY, tag);
  set({ dismissed: tag });
}

export function clearOutcome() {
  set({ outcome: null });
}
