"use client";

import { useEffect, useState } from "react";

import {
  deleteSchedule,
  listScheduleRuns,
  runScheduleNow,
  type ScheduleRun,
  type ScheduleView,
  setScheduleEnabled,
  updateSchedule,
} from "@/lib/api";
import { useDaemonEvents } from "@/lib/live";
import kanban from "../kanban/kanban.module.css";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import { ScheduleForm } from "./ScheduleForm";
import { alertOf, recurrenceText, RUN_LABELS, runTone, when } from "./scheduling";
import styles from "./schedules.module.css";

// How many occurrences the drawer shows.
const HISTORY = 50;

interface Props {
  view: ScheduleView;
  onClose: () => void;
  onOpenChat: (address: string) => void;
  onOpenTicket: (id: string) => void;
}

// ScheduleDrawer shows everything about a schedule: when and for whom it
// runs, its instructions, what is wrong with it, and its history; the human
// edits, pauses, resumes, runs or deletes it from here.
export function ScheduleDrawer({ view, onClose, onOpenChat, onOpenTicket }: Props) {
  const [runs, setRuns] = useState<ScheduleRun[]>([]);
  const [version, setVersion] = useState(0);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listScheduleRuns(view.id, HISTORY)
      .then((r) => !cancelled && setRuns(r))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, [view.id, version]);

  useDaemonEvents((e) => {
    if (e.type === "schedules.changed") setVersion((v) => v + 1);
  });

  async function act(action: () => Promise<unknown>, after?: () => void) {
    setBusy(true);
    try {
      await action();
      setError(null);
      after?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const alert = alertOf(view);
  const running = runs.some((r) => r.status === "waiting" || r.status === "running");

  return (
    <aside className={kanban.drawer} role="dialog" aria-label={`Schedule ${view.id}`}>
      <div className={kanban.drawerBody}>
        <div className={kanban.drawerHead}>
          <span className={kanban.id}>{view.id}</span>
          <button type="button" className={ui.link} onClick={onClose}>
            Close
          </button>
        </div>
        {editing ? (
          <ScheduleForm
            initial={view}
            idLocked
            submitLabel="Save"
            onSubmit={(s) => updateSchedule(view.id, s).then(() => setEditing(false))}
            onCancel={() => setEditing(false)}
          />
        ) : (
          <>
            <h3 className={ui.title}>{view.title}</h3>
            <p className={ui.sub}>
              {recurrenceText(view)} · {view.mode} ·{" "}
              {view.target.startsWith("@") ? (
                view.target
              ) : (
                <button type="button" className={ui.link} onClick={() => onOpenChat(view.target)}>
                  {view.target}
                </button>
              )}
            </p>
            <p className={ui.sub}>{view.enabled ? `Next: ${when(view.next_run)}` : "Paused: no occurrence until it is resumed."}</p>
            {alert && <p className={ui.error}>{alert}</p>}
            {view.warning && <p className={styles.warning}>{view.warning}</p>}
            <div className={styles.actions}>
              <button type="button" className={ui.btn} disabled={busy} onClick={() => setEditing(true)}>
                Edit
              </button>
              <button type="button" className={ui.btn} disabled={busy} onClick={() => void act(() => setScheduleEnabled(view.id, !view.enabled))}>
                {view.enabled ? "Pause" : "Resume"}
              </button>
              <button type="button" className={ui.btn} disabled={busy || running} title={running ? "An occurrence is not over yet" : undefined} onClick={() => void act(() => runScheduleNow(view.id))}>
                Run now
              </button>
              <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => setDeleting(true)}>
                Delete
              </button>
            </div>
            {error && <p className={ui.error}>{error}</p>}
            <h4 className={kanban.sectionTitle}>Instructions</h4>
            <p className={kanban.text}>{view.instructions}</p>
            <h4 className={kanban.sectionTitle}>History</h4>
            {runs.length === 0 && <p className={ui.sub}>No occurrence yet.</p>}
            <ul className={kanban.list}>
              {runs.map((r) => (
                <li key={r.id}>
                  <span className={styles.runLine}>
                    <span className={styles.runStatus} data-tone={runTone(r.status)}>
                      {RUN_LABELS[r.status]}
                    </span>
                    <span>{when(r.due_at)}</span>
                    {r.trigger === "manual" && <span className={ui.chip}>run now</span>}
                  </span>
                  <span className={kanban.by}>
                    {r.agent && <>for {r.agent} · </>}
                    {r.ticket && (
                      <button type="button" className={ui.link} onClick={() => onOpenTicket(r.ticket as string)}>
                        {r.ticket}
                      </button>
                    )}
                    {r.message && "mail"}
                    {r.ended_at && <> · ended {when(r.ended_at)}</>}
                  </span>
                  {r.note && <span className={kanban.by}>{r.note}</span>}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
      {deleting && (
        <Modal label={`Delete ${view.id}`} tone="danger" onClose={() => setDeleting(false)}>
          <h3 className={ui.title}>Delete {view.title}?</h3>
          <p className={ui.sub}>It is removed from .nobody/schedules.yaml (commit the change). Its history stays. An occurrence already running goes on to its end.</p>
          <div className={styles.actions}>
            <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void act(() => deleteSchedule(view.id), onClose)}>
              Delete
            </button>
            <button type="button" className={ui.link} onClick={() => setDeleting(false)}>
              Cancel
            </button>
          </div>
        </Modal>
      )}
    </aside>
  );
}
