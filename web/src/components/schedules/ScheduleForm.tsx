"use client";

import { type FormEvent, useEffect, useState } from "react";

import { previewRecurrence, SCHEDULE_ID_PATTERN, type Schedule, type ScheduleMode } from "@/lib/api";
import { useLive } from "@/lib/live";
import { isDirector } from "../chat/agents";
import ui from "../ui/ui.module.css";
import { PRESETS, when } from "./scheduling";
import styles from "./schedules.module.css";

// How long the form waits after the last keystroke before asking the daemon
// for the next occurrences.
const PREVIEW_DELAY_MS = 300;

interface Props {
  // The schedule to edit, or the proposal to accept; empty for a new one.
  initial?: Schedule;
  // The id cannot change once the schedule exists.
  idLocked?: boolean;
  submitLabel: string;
  onSubmit: (s: Schedule) => Promise<unknown>;
  onCancel: () => void;
}

const empty: Schedule = { id: "", title: "", every: "10m", target: "", mode: "message", instructions: "" };

// ScheduleForm creates or edits a scheduled task, or edits a proposal
// before accepting it. Under the recurrence, the daemon shows the next three
// occurrences (or what is wrong) while the human types.
export function ScheduleForm({ initial, idLocked = false, submitLabel, onSubmit, onCancel }: Props) {
  const [s, setS] = useState<Schedule>(initial ?? empty);
  const [kind, setKind] = useState<"every" | "cron">(initial?.cron ? "cron" : "every");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const set = (change: Partial<Schedule>) => setS((cur) => ({ ...cur, ...change }));

  const recurrence = kind === "every" ? { every: s.every ?? "" } : { cron: s.cron ?? "" };
  const value = kind === "every" ? (s.every ?? "") : (s.cron ?? "");

  function pickKind(k: "every" | "cron") {
    setKind(k);
    set(k === "every" ? { every: s.every || "10m", cron: undefined } : { cron: s.cron || "0 9 * * 1-5", every: undefined });
  }

  function pickPreset(p: (typeof PRESETS)[number]) {
    setKind(p.every ? "every" : "cron");
    set({ every: p.every, cron: p.cron });
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await onSubmit({ ...s, ...(kind === "every" ? { cron: undefined } : { every: undefined }) });
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)}>
      <div className={styles.formRow}>
        <label className={ui.label}>
          Id
          <input
            className={ui.input}
            value={s.id}
            disabled={idLocked}
            required
            pattern={SCHEDULE_ID_PATTERN}
            placeholder="grafana-watch"
            title="Lowercase letters, digits and dashes"
            onChange={(e) => set({ id: e.target.value })}
          />
        </label>
        <label className={ui.label}>
          Title
          <input className={ui.input} value={s.title} required placeholder="Watch the production dashboard" onChange={(e) => set({ title: e.target.value })} />
        </label>
      </div>

      <div className={ui.label}>
        When
        <div className={styles.recurrence}>
          <div className={ui.seg} role="group" aria-label="Recurrence">
            <button type="button" aria-pressed={kind === "every"} onClick={() => pickKind("every")}>
              Every
            </button>
            <button type="button" aria-pressed={kind === "cron"} onClick={() => pickKind("cron")}>
              Cron
            </button>
          </div>
          <input
            className={ui.input}
            aria-label={kind === "every" ? "Interval" : "Cron expression"}
            value={value}
            placeholder={kind === "every" ? "10m, 2h, 1h30m" : "0 9 * * 1-5"}
            onChange={(e) => set(kind === "every" ? { every: e.target.value } : { cron: e.target.value })}
          />
        </div>
        <div className={styles.presets}>
          {PRESETS.map((p) => (
            <button key={p.label} type="button" className={`${ui.chip} ${p.every === recurrence.every && p.cron === recurrence.cron ? ui.chipOn : ""}`} onClick={() => pickPreset(p)}>
              {p.label}
            </button>
          ))}
        </div>
        <Preview key={`${kind}:${value}`} every={recurrence.every} cron={recurrence.cron} />
      </div>

      <div className={styles.formRow}>
        <label className={ui.label}>
          Who
          <TargetSelect value={s.target} mode={s.mode} onChange={(target) => set({ target })} />
        </label>
        <div className={ui.label}>
          How
          <div className={ui.seg} role="group" aria-label="Mode">
            {(["message", "ticket"] as ScheduleMode[]).map((m) => (
              <button key={m} type="button" aria-pressed={s.mode === m} onClick={() => set({ mode: m })}>
                {m === "message" ? "Message" : "Ticket"}
              </button>
            ))}
          </div>
        </div>
      </div>
      <p className={ui.sub}>
        {s.mode === "message"
          ? "Mail from nobody, nothing on the Kanban: for work every few minutes or hours. The occurrence is over when the agent ends its turn."
          : "A ticket assigned to the agent, outside any feature, on the Kanban: for daily or weekly work. The occurrence is over when the ticket is done."}{" "}
        Each occurrence waits for a free agent, and is skipped while the previous one is not over.
      </p>

      <label className={ui.label}>
        Instructions
        <textarea
          className={ui.textarea}
          value={s.instructions}
          required
          placeholder="Open the dashboard with the Grafana MCP server. If an alert is red, write to human with what you see; otherwise do nothing."
          onChange={(e) => set({ instructions: e.target.value })}
        />
      </label>
      <p className={ui.sub}>Occurrences come from nobody, and nobody reads an answer: say when the agent should write to you.</p>

      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.formActions}>
        <button type="submit" className={ui.btnPrimary} disabled={busy}>
          {submitLabel}
        </button>
        <button type="button" className={ui.link} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

// Preview shows the next occurrences, or why the recurrence is wrong. It is
// keyed by the recurrence, so a new one starts from scratch.
function Preview({ every, cron }: { every?: string; cron?: string }) {
  const [next, setNext] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      previewRecurrence({ every, cron })
        .then((n) => !cancelled && setNext(n))
        .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    }, PREVIEW_DELAY_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [every, cron]);

  if (error) return <p className={styles.previewError}>{error.replace(/^invalid schedule: /, "")}</p>;
  if (!next) return <p className={styles.preview}>…</p>;
  return <p className={styles.preview}>Next: {next.map(when).join(" · ")} (this computer&apos;s time; the daemon uses its machine&apos;s time zone)</p>;
}

// TargetSelect offers the teams (their first free member), their agents,
// and the director, who can only receive mail.
function TargetSelect({ value, mode, onChange }: { value: string; mode: ScheduleMode; onChange: (target: string) => void }) {
  const { agents, teams } = useLive();
  const director = agents.find(isDirector);

  return (
    <select className={ui.select} value={value} required onChange={(e) => onChange(e.target.value)}>
      <option value="" disabled>
        Choose an agent or a team
      </option>
      {teams.map((t) => (
        <optgroup key={t.id} label={`Team ${t.id}`}>
          <option value={`@${t.id}`}>@{t.id} · its first free member</option>
          {agents
            .filter((a) => a.team === t.id)
            .map((a) => (
              <option key={a.address} value={a.address}>
                {a.address} · {a.role}
                {a.on_leave ? " (on leave)" : ""}
              </option>
            ))}
        </optgroup>
      ))}
      {director && (
        <optgroup label="Director">
          <option value={director.address} disabled={mode === "ticket"}>
            {director.address}
            {mode === "ticket" ? " · message mode only" : ""}
          </option>
        </optgroup>
      )}
    </select>
  );
}
