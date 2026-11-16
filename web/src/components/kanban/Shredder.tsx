"use client";

import { type DragEvent, useState } from "react";

import { deleteTicket, type Ticket } from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";

interface Props {
  tickets: Ticket[];
  onShredded: (ids: string[]) => void;
}

// Shredder deletes tickets for good: drop a card on it, confirm, and it is
// gone with its comments and deliverables. An agent working on it is told to
// stop. The tickets that depend on it are either unblocked or, if the box is
// checked, shredded too (and theirs).
export function Shredder({ tickets, onShredded }: Props) {
  const [over, setOver] = useState(false);
  const [chosen, setChosen] = useState<Ticket | null>(null);
  const [cascade, setCascade] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [shredding, setShredding] = useState(0);

  function drop(e: DragEvent) {
    e.preventDefault();
    setOver(false);
    const ticket = tickets.find((t) => t.id === e.dataTransfer.getData("text/plain"));
    if (ticket) {
      setChosen(ticket);
      setCascade(false);
      setError(null);
    }
  }

  async function shred() {
    if (!chosen) return;
    setBusy(true);
    try {
      const ids = await deleteTicket(chosen.id, cascade);
      setChosen(null);
      setShredding((n) => n + 1);
      onShredded(ids);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const dependents = chosen ? dependentsOf(tickets, chosen.id) : [];

  return (
    <>
      <div
        className={styles.shredder}
        data-over={over}
        aria-label="Shredder: drop a ticket here to delete it"
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
      >
        <span className={styles.shredderSlot} />
        <span className={styles.shredderLabel}>SHREDDER</span>
        {/* Paper strips fall each time a ticket is shredded. */}
        <span key={shredding} className={shredding ? styles.strips : undefined} aria-hidden="true">
          {shredding > 0 && Array.from({ length: 7 }, (_, i) => <i key={i} style={{ left: `${10 + i * 11}px`, animationDelay: `${i * 60}ms` }} />)}
        </span>
      </div>

      {chosen && (
        <div className={styles.modalBackdrop}>
          <div className={styles.modal} role="dialog" aria-modal="true" aria-label={`Shred ${chosen.id}`}>
            <h3 className={ui.title}>Shred {chosen.id}?</h3>
            <p className={styles.modalTicket}>{chosen.title}</p>
            <p className={ui.sub}>The ticket, its comments and its deliverables are deleted for good. Messages about it stay.</p>
            {chosen.status === "in_progress" && chosen.assignee && (
              <p className={styles.modalWarning}>
                {chosen.assignee} is working on it: their job on this ticket is stopped (they get an urgent notice). Changes they did not commit stay in their
                worktree.
              </p>
            )}
            {dependents.length > 0 && (
              <label className={styles.modalCheck}>
                <input type="checkbox" checked={cascade} onChange={(e) => setCascade(e.target.checked)} />
                <span>
                  Also shred the tickets that depend on it, and theirs: {dependents.join(", ")}.
                  <br />
                  <em>{cascade ? "They are deleted too." : "Unchecked, they stop waiting for it."}</em>
                </span>
              </label>
            )}
            {error && <p className={ui.error}>{error}</p>}
            <div className={styles.actions}>
              <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void shred()}>
                {cascade && dependents.length ? `Shred ${dependents.length + 1} tickets` : `Shred ${chosen.id}`}
              </button>
              <button type="button" className={ui.link} onClick={() => setChosen(null)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

// dependentsOf lists the tickets that depend on id, directly or not (the
// same walk as the daemon's work.Dependents).
function dependentsOf(tickets: Ticket[], id: string): string[] {
  const waiting = new Map<string, string[]>();
  for (const t of tickets) for (const dep of t.blocked_by ?? []) waiting.set(dep, [...(waiting.get(dep) ?? []), t.id]);
  const seen = new Set([id]);
  const found: string[] = [];
  const queue = [id];
  while (queue.length) {
    for (const d of waiting.get(queue.shift() as string) ?? []) {
      if (!seen.has(d)) {
        seen.add(d);
        found.push(d);
        queue.push(d);
      }
    }
  }
  return found;
}
