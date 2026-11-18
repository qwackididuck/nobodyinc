"use client";

import { type FormEvent, useState } from "react";

import { createTicket, usd } from "@/lib/api";
import { useLive } from "@/lib/live";
import { parseUSD, useBudgetsOn } from "../budgets/CapEditor";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";

interface Props {
  onClose: () => void;
  onCreated: (id: string) => void;
}

// NewTicket files a ticket in a team's queue, as the human. It joins the
// active feature, if any; the team's next available agent takes it.
export function NewTicket({ onClose, onCreated }: Props) {
  const { teams, tickets, features } = useLive();
  const [title, setTitle] = useState("");
  const [team, setTeam] = useState(teams[0]?.id ?? "");
  const [description, setDescription] = useState("");
  const [criteria, setCriteria] = useState("");
  const [blockedBy, setBlockedBy] = useState<string[]>([]);
  const [budget, setBudget] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const unfinished = tickets.filter((t) => t.status === "open" || t.status === "in_progress");

  // Inside an active feature with a budget, every ticket needs one, at most
  // what the feature has left to allocate (ADR-0013).
  const budgetsOn = useBudgetsOn();
  const active = features.find((f) => f.active);
  const left = active?.budget_usd !== undefined ? active.budget_usd - active.allocated_usd : undefined;
  const amount = parseUSD(budget);
  const budgetRequired = budgetsOn && left !== undefined;
  const budgetOK = !budgetsOn || (amount === undefined ? !budgetRequired : !Number.isNaN(amount) && (left === undefined || amount <= left + 0.005));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const t = await createTicket({
        title: title.trim(),
        team,
        description,
        acceptance_criteria: criteria,
        blocked_by: blockedBy,
        ...(budgetsOn && amount !== undefined ? { budget_usd: amount } : {}),
      });
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
        {budgetsOn && (
          <label className={ui.label}>
            BUDGET (USD){budgetRequired ? "" : ", OPTIONAL"}
            <input
              id="ticket-budget"
              className={ui.input}
              inputMode="decimal"
              value={budget}
              required={budgetRequired}
              onChange={(e) => setBudget(e.target.value)}
            />
            {left !== undefined && (
              <span className={ui.sub}>
                {active?.id} has {usd(Math.max(left, 0))} left to allocate.
              </span>
            )}
          </label>
        )}
        <div className={styles.actions}>
          <button type="submit" className={ui.btnPrimary} disabled={busy || !title.trim() || !team || !budgetOK}>
            Create
          </button>
        </div>
        {error && <p className={ui.error}>{error}</p>}
      </form>
    </div>
  );
}
