"use client";

import "@xterm/xterm/css/xterm.css";

import { useEffect, useRef, useState } from "react";

import type { AgentStatus } from "@/lib/api";
import styles from "./chat.module.css";

// Terminal reports are answers the emulator sends back to queries found in the
// output (device attributes, cursor position, keyboard protocol, colours…).
// The daemon replays recent output when we attach, so xterm.js would answer
// stale queries and the agent would receive them as typed text. Keystrokes
// never look like this, so these reports are not forwarded.
const TERMINAL_REPORT =
  /^(?:\x1b\[[?>]?[\d;]*[cRu]|\x1b\[\?[\d;]*\$y|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\))+$/;

// How long to wait before trying to reach the agent's terminal again.
const RECONNECT_DELAY_MS = 1000;

interface Props {
  // The WebSocket of the terminal (terminalURL, shellTerminalURL).
  url: string;
  // The agent's status, to tell a restart from a stopped agent. Without it,
  // the terminal is not an agent's.
  status?: AgentStatus;
  // Called, instead of waiting for a next session, when the terminal's process
  // ended: a shell does not come back.
  onEnded?: () => void;
  // Whether the terminal is the one the human is using: it takes the keyboard
  // when it becomes so.
  active?: boolean;
}

// TerminalView shows an agent's live terminal and lets the human type into it.
// Protocol with the daemon: binary frames carry output; we send JSON text
// frames {"type":"input"} and {"type":"resize"} (see internal/runtime/terminal.go).
//
// The daemon closes the connection when the agent's process ends: a restart
// after an edit, a crash, a stop. The view then says so, keeps trying, and
// once the agent's next session is there, starts again from a clean screen.
export function TerminalView({ url, status, onEnded, active = true }: Props) {
  const container = useRef<HTMLDivElement>(null);
  const term = useRef<{ focus: () => void } | null>(null);
  // The latest callback and focus wish, read by the long-lived connection.
  const ended = useRef(onEnded);
  const wantsFocus = useRef(active);
  useEffect(() => {
    ended.current = onEnded;
    wantsFocus.current = active;
  });
  // Set while the agent's session is gone and the view waits for the next one.
  const [waiting, setWaiting] = useState(false);

  useEffect(() => {
    const element = container.current;
    if (!element) return;

    let disposed = false;
    let cleanup = () => {};

    // xterm.js needs the browser, so it is loaded on the client only.
    void (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]);
      if (disposed) return;

      const xterm = new Terminal({ fontSize: 13, cursorBlink: true, convertEol: false, scrollback: 5000 });
      const fit = new FitAddon();
      xterm.loadAddon(fit);
      xterm.open(element);
      fit.fit();
      term.current = xterm;
      if (wantsFocus.current) xterm.focus();

      let socket: WebSocket | null = null;
      let retry: ReturnType<typeof setTimeout> | undefined;
      // Whether a session was shown already: the next one starts on a clean screen.
      let attached = false;

      const send = (msg: object) => {
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
      };
      const sendSize = () => send({ type: "resize", rows: xterm.rows, cols: xterm.cols });

      const connect = () => {
        const current = new WebSocket(url);
        current.binaryType = "arraybuffer";
        socket = current;

        current.onopen = () => {
          if (attached) xterm.reset(); // the new session replays its own screen
          attached = true;
          setWaiting(false);
          // Resizing makes Claude Code redraw its screen: a clean picture on attach.
          sendSize();
        };
        current.onmessage = (event: MessageEvent<ArrayBuffer>) => xterm.write(new Uint8Array(event.data));
        // Ended session, or agent not running yet (the daemon refuses): wait, then try again.
        current.onclose = () => {
          if (disposed || socket !== current) return;
          if (ended.current) {
            ended.current();
            return;
          }
          setWaiting(true);
          retry = setTimeout(connect, RECONNECT_DELAY_MS);
        };
      };
      connect();

      const input = xterm.onData((data) => {
        if (!TERMINAL_REPORT.test(data)) send({ type: "input", data });
      });

      const observer = new ResizeObserver(() => {
        fit.fit();
        sendSize();
      });
      observer.observe(element);

      cleanup = () => {
        clearTimeout(retry);
        observer.disconnect();
        input.dispose();
        socket?.close();
        term.current = null;
        xterm.dispose();
      };
    })();

    return () => {
      disposed = true;
      cleanup();
    };
  }, [url]);

  useEffect(() => {
    if (active) term.current?.focus();
  }, [active]);

  return (
    <div className={styles.terminalFrame}>
      <div ref={container} className={styles.xterm} />
      {waiting && status && (
        <div className={styles.terminalWaiting} role="status">
          {status === "stopped" || status === "exited"
            ? "The agent is not running: its terminal comes back as soon as it starts."
            : "Restarting the agent… its terminal comes back with its next session."}
        </div>
      )}
    </div>
  );
}
