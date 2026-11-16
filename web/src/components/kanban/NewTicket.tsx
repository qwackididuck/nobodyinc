"use client";

import { type FormEvent, useState } from "react";

import { createTicket } from "@/lib/api";
import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";

interface Props {
  onClose: () => void;
  onCreated: (id: string) => void;
}

// NewTicket files a ticket in a team's queue, as the human. It joins the
// active feature, if any; the team's next available agent takes it.
export function NewTicket({ onClose, onCreated }: Props) {
  const { teams, tickets } = useLive();
  const [title, setTitle] = useState("");
  const [team, setTeam] = useState(teams[0]?.id ?? "");
  const [description, setDescription] = useState("");
  const [criteria, setCriteria] = useState("");
  const [blockedBy, setBlockedBy] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const unfinished = tickets.filter((t) => t.status === "open" || t.status === "in_progress");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const t = await createTicket({ title: title.trim(), team, description, acceptance_criteria: criteria, blocked_by: blockedBy });
      onClose();
      onCreated(t.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.drawer} role="dialog" aria-label="New ticket">
      <form className={styles.drawerBody} onSubmit={submit}>
        <div className={styles.drawerHead}>
          <h3 className={ui.title}>New ticket</h3>
          <button type="button" className={ui.link} onClick={onClose}>
            Close
          </button>
        </div>
        <label className={ui.label}>
          TITLE
          <input id="ticket-title" className={ui.input} value={title} onChange={(e) => setTitle(e.target.value)} required />
        </label>
        <label className={ui.label}>
          TEAM
          <select id="ticket-team" className={ui.select} value={team} onChange={(e) => setTeam(e.target.value)}>
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.id}
              </option>
            ))}
          </select>
        </label>
        <label className={ui.label}>
          WHAT IS NEEDED
          <textarea id="ticket-description" className={ui.textarea} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <label className={ui.label}>
          ACCEPTANCE CRITERIA, ONE PER LINE
          <textarea id="ticket-criteria" className={ui.textarea} value={criteria} onChange={(e) => setCriteria(e.target.value)} />
        </label>
        {unfinished.length > 0 && (
          <div className={ui.label}>
            WAITS FOR
            <div className={styles.chips}>
              {unfinished.map((t) => {
                const on = blockedBy.includes(t.id);
                return (
                  <button
                    key={t.id}
                    type="button"
                    className={`${ui.chip} ${on ? ui.chipOn : ""}`}
                    aria-pressed={on}
                    title={t.title}
                    onClick={() => setBlockedBy(on ? blockedBy.filter((x) => x !== t.id) : [...blockedBy, t.id])}
                  >
                    {t.id}
                  </button>
                );
              })}
            </div>
          </div>
        )}
        <div className={styles.actions}>
          <button type="submit" className={ui.btnPrimary} disabled={busy || !title.trim() || !team}>
            Create
          </button>
        </div>
        {error && <p className={ui.error}>{error}</p>}
      </form>
    </div>
  );
}
