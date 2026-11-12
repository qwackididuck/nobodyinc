"use client";

import "@xterm/xterm/css/xterm.css";

import { useEffect, useRef } from "react";

import { terminalURL } from "@/lib/api";
import styles from "./ControlRoom.module.css";

// Terminal reports are answers the emulator sends back to queries found in the
// output (device attributes, cursor position, keyboard protocol, colours…).
// The daemon replays recent output when we attach, so xterm.js would answer
// stale queries and the agent would receive them as typed text. Keystrokes
// never look like this, so these reports are not forwarded.
const TERMINAL_REPORT =
  /^(?:\x1b\[[?>]?[\d;]*[cRu]|\x1b\[\?[\d;]*\$y|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\))+$/;

interface Props {
  address: string;
}

// TerminalView shows an agent's live terminal and lets the human type into it.
// Protocol with the daemon: binary frames carry output; we send JSON text
// frames {"type":"input"} and {"type":"resize"} (see internal/runtime/terminal.go).
export function TerminalView({ address }: Props) {
  const container = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = container.current;
    if (!element) return;

    let disposed = false;
    let cleanup = () => {};

    // xterm.js needs the browser, so it is loaded on the client only.
    void (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]);
      if (disposed) return;

      const term = new Terminal({ fontSize: 13, cursorBlink: true, convertEol: false, scrollback: 5000 });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(element);
      fit.fit();

      const socket = new WebSocket(terminalURL(address));
      socket.binaryType = "arraybuffer";

      const send = (msg: object) => {
        if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
      };
      const sendSize = () => send({ type: "resize", rows: term.rows, cols: term.cols });

      // Resizing makes Claude Code redraw its screen: a clean picture on attach.
      socket.onopen = sendSize;
      socket.onmessage = (event: MessageEvent<ArrayBuffer>) => term.write(new Uint8Array(event.data));
      socket.onclose = () => term.write("\r\n\x1b[2m[disconnected from the agent's terminal]\x1b[0m\r\n");

      const input = term.onData((data) => {
        if (!TERMINAL_REPORT.test(data)) send({ type: "input", data });
      });

      const observer = new ResizeObserver(() => {
        fit.fit();
        sendSize();
      });
      observer.observe(element);

      cleanup = () => {
        observer.disconnect();
        input.dispose();
        socket.close();
        term.dispose();
      };
    })();

    return () => {
      disposed = true;
      cleanup();
    };
  }, [address]);

  return <div ref={container} className={styles.terminal} />;
}
