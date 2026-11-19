"use client";

// One window, one project (ADR-0014). The project is in the window's URL
// (?project=<id>); the window holds it through a WebSocket to the launcher
// for as long as it shows it. The project's daemon runs while it is held and
// stops when the window closes (after a grace period, so that a reload does
// not stop it) or switches project (at once).

import { useCallback, useEffect, useRef, useState } from "react";

import { attachURL, openDesktopWindow } from "./launcher";

// projectURL is the address of a project's window ("" for the projects page).
// setup marks the first opening of a project the launcher has just set up:
// the control room then offers to import its subagents (ADR-0018).
export function projectURL(id: string | null, setup = false): string {
  if (!id) return "/";
  return `/?project=${encodeURIComponent(id)}${setup ? "&setup=1" : ""}`;
}

const WINDOW_KEY = "nobody.window";
const DESKTOP_KEY = "nobody.desktop";

// windowID identifies this window to the launcher. It survives a reload (the
// same window gets its hold back within the grace period), so it is kept in
// sessionStorage, which is per tab. A duplicated tab copies it: the launcher
// then gives the project to the newest connection. In the desktop app, the
// app chooses it (?window=<id> in the window's first URL, ADR-0016), so that
// closing the window releases its project at once.
export function windowID(): string {
  try {
    let id = window.sessionStorage.getItem(WINDOW_KEY);
    if (!id) {
      id = new URLSearchParams(window.location.search).get("window") ?? crypto.randomUUID();
      window.sessionStorage.setItem(WINDOW_KEY, id);
    }
    return id;
  } catch {
    // No sessionStorage: a new id per page load, the hold is lost on reload.
    return (windowIDFallback ??= crypto.randomUUID());
  }
}

let windowIDFallback: string | undefined;

// inDesktopApp reports whether this page runs in a window of the desktop app
// (?desktop=1 in the window's first URL, kept for the window's lifetime).
export function inDesktopApp(): boolean {
  try {
    if (new URLSearchParams(window.location.search).get("desktop") === "1") {
      window.sessionStorage.setItem(DESKTOP_KEY, "1");
    }
    return window.sessionStorage.getItem(DESKTOP_KEY) === "1";
  } catch {
    return false;
  }
}

// openWindow opens url (projectURL) in a new window: a browser tab, or in
// the desktop app a new native window, since a web view has no tabs.
export function openWindow(url: string): void {
  if (!inDesktopApp()) {
    window.open(url, "_blank");
    return;
  }
  const project = new URL(url, window.location.href).searchParams.get("project");
  void openDesktopWindow(project ?? undefined);
}

// The window's hold on its project, as the control room shows it:
//   - opening: connecting, or the daemon is starting;
//   - running: the office can be shown;
//   - crashed: the daemon stopped on its own (error says why);
//   - held: another window has the project (we may take it over);
//   - shownElsewhere: another window has the project, and the desktop app
//     brought it to the front (this window goes back to the projects page);
//   - taken: another window took the project from this one;
//   - reconnecting: the connection to the launcher dropped, retrying.
export type HoldStatus = "opening" | "running" | "crashed" | "held" | "shownElsewhere" | "taken" | "reconnecting";

export interface Hold {
  status: HoldStatus;
  error?: string;
  // takeOver holds the project although another window has it.
  takeOver: () => void;
  // restart starts the daemon again after it crashed.
  restart: () => void;
  // release lets the project go at once (its daemon stops), then resolves.
  release: () => Promise<void>;
}

// Code the launcher closes with when another window has the project.
const CLOSE_HELD = 4409;
const RETRY_MS = [500, 1000, 2000, 5000];
// release waits at most this long for the launcher to close the connection.
const RELEASE_TIMEOUT_MS = 2000;

type LauncherMessage =
  | { type: "state"; state: "stopped" | "starting" | "running" | "stopping"; error?: string }
  | { type: "held"; focused?: boolean }
  | { type: "taken" };

// useHold holds project from this window while the component is mounted;
// with takeover, the first connection takes it from the window that has it.
export function useHold(project: string, takeover = false): Hold {
  const [status, setStatus] = useState<HoldStatus>("opening");
  const [error, setError] = useState<string>();
  // Bumped to connect again; takeover says whether the next connection takes
  // the project over.
  const [attempt, setAttempt] = useState({ n: 0, takeover });
  const socket = useRef<WebSocket | null>(null);
  // Set by release: the connection's end is not a drop to recover from.
  const released = useRef(false);

  useEffect(() => {
    let retries = 0;
    let closedOnPurpose = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const connect = (takeover: boolean) => {
      const ws = new WebSocket(attachURL(project, windowID(), takeover));
      socket.current = ws;

      ws.onmessage = (e) => {
        const msg = JSON.parse(String(e.data)) as LauncherMessage;
        retries = 0;
        if (msg.type === "held") {
          setStatus(msg.focused ? "shownElsewhere" : "held");
        } else if (msg.type === "taken") {
          setStatus("taken");
        } else if (msg.state === "running") {
          setStatus("running");
          setError(undefined);
        } else if (msg.state === "stopped" && msg.error) {
          setStatus("crashed");
          setError(msg.error);
        } else {
          setStatus("opening");
        }
      };

      ws.onclose = (e) => {
        if (closedOnPurpose || released.current || e.code === CLOSE_HELD) return;
        // The launcher restarted, the network dropped, the laptop woke up:
        // reconnect with the same window id to get the hold back.
        setStatus("reconnecting");
        timer = setTimeout(() => connect(false), RETRY_MS[Math.min(retries++, RETRY_MS.length - 1)]);
      };
    };

    connect(attempt.takeover);

    return () => {
      closedOnPurpose = true;
      clearTimeout(timer);
      socket.current?.close();
    };
  }, [project, attempt]);

  const takeOver = useCallback(() => {
    setStatus("opening");
    setAttempt((a) => ({ n: a.n + 1, takeover: true }));
  }, []);

  const restart = useCallback(() => {
    setStatus("opening");
    if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify({ type: "start" }));
  }, []);

  const release = useCallback(
    () =>
      new Promise<void>((resolve) => {
        const ws = socket.current;
        released.current = true;
        if (!ws || ws.readyState !== WebSocket.OPEN) return resolve();
        ws.addEventListener("close", () => resolve(), { once: true });
        setTimeout(resolve, RELEASE_TIMEOUT_MS);
        ws.send(JSON.stringify({ type: "release" }));
      }),
    [],
  );

  return { status, error, takeOver, restart, release };
}
