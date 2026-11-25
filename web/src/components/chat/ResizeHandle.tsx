"use client";

import { useRef } from "react";

import styles from "./chat.module.css";

interface Props {
  width: number;
  onResize: (w: number) => void;
  // onDone is called with the final width, to remember it.
  onDone: (w: number) => void;
}

// ResizeHandle is the chat panel's right edge: drag it right to widen the
// panel, left to narrow it. Arrow keys do the same.
export function ResizeHandle({ width, onResize, onDone }: Props) {
  const start = useRef<{ x: number; w: number } | null>(null);
  const last = useRef(width);

  return (
    <div
      className={styles.resize}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the chat"
      aria-valuenow={width}
      tabIndex={0}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        start.current = { x: e.clientX, w: width };
        last.current = width;
      }}
      onPointerMove={(e) => {
        if (!start.current) return;
        last.current = start.current.w + e.clientX - start.current.x;
        onResize(last.current);
      }}
      onPointerUp={() => {
        if (!start.current) return;
        start.current = null;
        onDone(last.current);
      }}
      onKeyDown={(e) => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        const next = width + (e.key === "ArrowRight" ? 24 : -24);
        onResize(next);
        onDone(next);
      }}
    />
  );
}
