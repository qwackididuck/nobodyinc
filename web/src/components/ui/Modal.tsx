"use client";

import { type ReactNode, useEffect } from "react";
import { createPortal } from "react-dom";

import ui from "./ui.module.css";

interface Props {
  label: string;
  // danger frames the dialog in red, for destructive confirmations.
  tone?: "default" | "danger";
  // wide makes room for a table (the subagents import).
  size?: "default" | "wide";
  onClose: () => void;
  children: ReactNode;
}

// Modal is a dialog over the whole control room, rendered at the end of the
// page (a portal) so that it never lands inside another form or inherits the
// styles of where it was opened. Escape and a click on the backdrop close it.
// React events still bubble to the components that opened it: a form inside
// must stop its submit event.
export function Modal({ label, tone = "default", size = "default", onClose, children }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return createPortal(
    <div className={ui.modalBackdrop} onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className={ui.modal} role="dialog" aria-modal="true" aria-label={label} data-tone={tone} data-size={size}>
        {children}
      </div>
    </div>,
    document.body,
  );
}
