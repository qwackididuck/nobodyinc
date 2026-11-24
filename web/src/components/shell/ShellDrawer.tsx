"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { type Shell, closeShell, listShells, openShell, shellTerminalURL } from "@/lib/api";
import { TerminalView } from "../chat/TerminalView";
import styles from "./shell.module.css";

// How tall the drawer may be, in pixels: it never hides the top bar entirely.
const MIN_HEIGHT = 140;
const DEFAULT_HEIGHT = 320;
const HEIGHT_KEY = "nobody.shellDrawer.height";

// The height the human chose last time. The control room only renders in the
// browser, so storage is there to read when the drawer first renders.
function savedHeight(): number {
  try {
    const saved = Number(window.localStorage.getItem(HEIGHT_KEY));
    if (saved >= MIN_HEIGHT) return saved;
  } catch {
    // storage unavailable: the default height will do
  }
  return DEFAULT_HEIGHT;
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// ShellDrawer is the plain terminals of the project (ADR-0021), in a drawer at
// the bottom of the control room: one tab per shell, a handle to resize it,
// and a button to take the whole main area. The shells live in the daemon, so
// reloading the page finds them again.
//
// It stays mounted while closed, hidden: the terminals keep their screens.
export function ShellDrawer({ open, onOpenChange }: Props) {
  const [shells, setShells] = useState<Shell[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [maximized, setMaximized] = useState(false);
  const [height, setHeight] = useState(savedHeight);
  const [error, setError] = useState<string | null>(null);
  // The shell shown: the one chosen, else the last (the chosen one is gone).
  const activeId = (shells.find((s) => s.id === chosenId) ?? shells.at(-1))?.id ?? null;
  // Set while a shell is being opened, so that nothing opens it twice.
  const opening = useRef(false);

  // The shells already open (a reload).
  useEffect(() => {
    let cancelled = false;
    listShells()
      .then((list) => {
        if (cancelled) return;
        setShells(list);
      })
      .catch((e: unknown) => !cancelled && setError(message(e)))
      .finally(() => !cancelled && setLoaded(true));
    return () => {
      cancelled = true;
    };
  }, []);

  const add = useCallback(async () => {
    if (opening.current) return;
    opening.current = true;
    try {
      const shell = await openShell();
      setShells((list) => [...list, shell]);
      setChosenId(shell.id);
      setError(null);
    } catch (e) {
      setError(message(e));
    } finally {
      opening.current = false;
    }
  }, []);

  // When the last shell is gone (closed, or ended by itself), so is the
  // drawer; emptied tells the next effect not to open a new one at once.
  const hadShells = useRef(false);
  const emptied = useRef(false);
  useEffect(() => {
    if (shells.length > 0) {
      hadShells.current = true;
    } else if (hadShells.current) {
      hadShells.current = false;
      emptied.current = true;
      setMaximized(false);
      onOpenChange(false);
    }
  }, [shells.length, onOpenChange]);

  // Opening the drawer with no shell opens one.
  useEffect(() => {
    if (!open) emptied.current = false;
    else if (loaded && shells.length === 0 && !error && !emptied.current) void add();
  }, [open, loaded, shells.length, error, add]);

  // A shell is gone: its tab goes with it.
  const forget = useCallback((id: string) => setShells((list) => list.filter((s) => s.id !== id)), []);

  async function close(id: string) {
    try {
      await closeShell(id);
    } catch (e) {
      // Already gone is the wished outcome; anything else is worth saying.
      if (!/unknown shell/.test(message(e))) setError(message(e));
    }
    forget(id);
  }

  // Dragging the handle changes the height; the terminals refit by themselves.
  function startResize(event: React.PointerEvent<HTMLDivElement>) {
    event.preventDefault();
    const startY = event.clientY;
    const startHeight = height;
    let latest = startHeight;
    const maxHeight = () => window.innerHeight - 120;

    const move = (e: PointerEvent) => {
      latest = Math.min(maxHeight(), Math.max(MIN_HEIGHT, startHeight + startY - e.clientY));
      setHeight(latest);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      try {
        window.localStorage.setItem(HEIGHT_KEY, String(Math.round(latest)));
      } catch {
        // not remembered: the next visit starts at the default height
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  return (
    <section
      className={styles.drawer}
      data-maximized={maximized}
      hidden={!open}
      style={maximized ? undefined : { height }}
      aria-label="Terminal"
    >
      {!maximized && <div className={styles.drawerHandle} onPointerDown={startResize} role="separator" aria-orientation="horizontal" aria-label="Resize the terminal" />}
      <div className={styles.drawerBar}>
        <div className={styles.shellTabs} role="tablist" aria-label="Shells">
          {shells.map((s) => (
            <span key={s.id} className={styles.shellTab} data-active={s.id === activeId}>
              <button type="button" role="tab" aria-selected={s.id === activeId} className={styles.shellTabName} onClick={() => setChosenId(s.id)}>
                {s.name}
              </button>
              <button type="button" className={styles.shellTabClose} onClick={() => void close(s.id)} aria-label={`Close ${s.name}`} title={`Close ${s.name}`}>
                ×
              </button>
            </span>
          ))}
          <button type="button" className={styles.shellAdd} onClick={() => void add()} aria-label="New shell" title="New shell">
            +
          </button>
        </div>
        {error && (
          <span className={styles.drawerError} role="alert">
            {error}
          </span>
        )}
        <button type="button" className={styles.drawerBtn} onClick={() => setMaximized((m) => !m)} aria-pressed={maximized}>
          {maximized ? "Reduce" : "Expand"}
        </button>
        <button type="button" className={styles.drawerBtn} onClick={() => onOpenChange(false)} aria-label="Hide the terminal" title="Hide (Ctrl+`). The shells keep running.">
          Hide
        </button>
      </div>
      <div className={styles.drawerBody}>
        {shells.map((s) => (
          <div key={s.id} className={styles.shellPane} hidden={s.id !== activeId}>
            <TerminalView url={shellTerminalURL(s.id)} active={open && s.id === activeId} onEnded={() => forget(s.id)} />
          </div>
        ))}
      </div>
    </section>
  );
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
