"use client";

import { useState } from "react";

import {
  acceptScheduleProposal,
  createSchedule,
  refuseScheduleProposal,
  runScheduleNow,
  type ScheduleProposal,
  type ScheduleView,
  setScheduleEnabled,
} from "@/lib/api";
import { useLive } from "@/lib/live";
import kanban from "../kanban/kanban.module.css";
import { TicketDrawer } from "../kanban/TicketDrawer";
import ui from "../ui/ui.module.css";
import type { ScheduleFocus } from "./nav";
import { ScheduleDrawer } from "./ScheduleDrawer";
import { ScheduleForm } from "./ScheduleForm";
import { alertOf, recurrenceText, RUN_LABELS, runTone, short } from "./scheduling";
import styles from "./schedules.module.css";

// What the right side shows: a schedule, the form of a new one, a proposal
// being edited before it is accepted, or a ticket of an occurrence.
type Panel = { kind: "schedule"; id: string } | { kind: "new" } | { kind: "proposal"; id: string } | { kind: "ticket"; id: string; from: string };

interface Props {
  // What to show first, when opened from elsewhere (the director's chat,
  // the office clock). The tab is keyed by it, so a new focus starts over.
  focus?: ScheduleFocus;
  onOpenChat: (address: string) => void;
}

// Schedules is the tab of the scheduled tasks (ADR-0012): the director's
// proposals waiting for the human, then one row per schedule with its
// recurrence, target, mode, next and last occurrences. A row opens its
// drawer (details, history, actions).
export function Schedules({ focus, onOpenChat }: Props) {
  const { schedules, scheduleProposals, loaded } = useLive();
  const [panel, setPanel] = useState<Panel | null>(focus ?? null);
  const pending = scheduleProposals.filter((p) => p.status === "proposed");

  const openView = panel?.kind === "schedule" ? schedules.find((s) => s.id === panel.id) : undefined;
  const openProposal = panel?.kind === "proposal" ? pending.find((p) => p.id === panel.id) : undefined;

  return (
    <div className={kanban.frame}>
      <div className={ui.scroll}>
        <div className={styles.page}>
          <div className={styles.bar}>
            <div>
              <h2 className={ui.title}>Schedules</h2>
              <p className={ui.sub}>
                Recurring work the daemon hands to agents while it runs, signed by nobody. Written to .nobody/schedules.yaml: commit it.
              </p>
            </div>
            <button type="button" className={ui.btnPrimary} onClick={() => setPanel({ kind: "new" })}>
              + New schedule
            </button>
          </div>

          {pending.length > 0 && (
            <section className={styles.proposals} aria-label="Proposals from the director">
              <span className={styles.kicker}>Proposals from the director ({pending.length})</span>
              {pending.map((p) => (
                <ProposalRow key={p.id} proposal={p} onEdit={() => setPanel({ kind: "proposal", id: p.id })} />
              ))}
            </section>
          )}

          {loaded.schedules && schedules.length === 0 ? (
            <p className={styles.empty}>No scheduled task yet. Create one, or ask the director to propose one.</p>
          ) : (
            <table className={styles.table}>
              <thead>
                <tr>
                  <th aria-label="State" />
                  <th>Task</th>
                  <th>When</th>
                  <th>Who</th>
                  <th>How</th>
                  <th>Next</th>
                  <th>Last</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {schedules.map((v) => (
                  <Row key={v.id} view={v} selected={openView?.id === v.id} onOpen={() => setPanel({ kind: "schedule", id: v.id })} />
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {openView && (
        <ScheduleDrawer
          key={openView.id}
          view={openView}
          onClose={() => setPanel(null)}
          onOpenChat={onOpenChat}
          onOpenTicket={(id) => setPanel({ kind: "ticket", id, from: openView.id })}
        />
      )}
      {panel?.kind === "new" && (
        <FormDrawer title="New schedule" onClose={() => setPanel(null)}>
          <ScheduleForm
            submitLabel="Create"
            onSubmit={(s) => createSchedule(s).then((v) => setPanel({ kind: "schedule", id: v.id }))}
            onCancel={() => setPanel(null)}
          />
        </FormDrawer>
      )}
      {openProposal && (
        <FormDrawer title={`Proposal ${openProposal.id} from ${openProposal.proposed_by}`} onClose={() => setPanel(null)}>
          {openProposal.warning && <p className={styles.warning}>{openProposal.warning}</p>}
          <ScheduleForm
            initial={openProposal.schedule}
            submitLabel="Accept"
            onSubmit={(s) => acceptScheduleProposal(openProposal.id, s).then(() => setPanel({ kind: "schedule", id: s.id }))}
            onCancel={() => setPanel(null)}
          />
        </FormDrawer>
      )}
      {panel?.kind === "ticket" && (
        <TicketDrawer id={panel.id} onClose={() => setPanel({ kind: "schedule", id: panel.from })} onOpenChat={onOpenChat} />
      )}
    </div>
  );
}

// Row is one schedule in the table.
function Row({ view: v, selected, onOpen }: { view: ScheduleView; selected: boolean; onOpen: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const alert = alertOf(v);
  const last = v.last_run;
  const busy = last?.status === "waiting" || last?.status === "running";

  async function act(action: () => Promise<unknown>) {
    try {
      await action();
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <tr className={styles.row} data-alert={alert !== null} data-paused={!v.enabled} aria-selected={selected} onClick={onOpen}>
      <td>
        <span className={styles.state} data-on={v.enabled} title={v.enabled ? "Enabled" : "Paused"} />
      </td>
      <td>
        <strong>{v.title}</strong>
        <span className={styles.rowSub}>{v.id}</span>
        {alert && <span className={styles.rowAlert}>{alert}</span>}
        {error && <span className={styles.rowAlert}>{error}</span>}
      </td>
      <td>{recurrenceText(v)}</td>
      <td>
        <code>{v.target}</code>
      </td>
      <td>{v.mode}</td>
      <td className={styles.nowrap}>{v.enabled ? short(v.next_run) : "paused"}</td>
      <td>
        {last ? (
          <span className={styles.runStatus} data-tone={runTone(last.status)} title={last.note}>
            {RUN_LABELS[last.status]} · {short(last.ended_at ?? last.due_at)}
          </span>
        ) : (
          "—"
        )}
      </td>
      <td className={styles.rowActions} onClick={(e) => e.stopPropagation()}>
        <button type="button" className={ui.btn} disabled={busy} title={busy ? "An occurrence is not over yet" : "Run one occurrence now"} onClick={() => void act(() => runScheduleNow(v.id))}>
          Run now
        </button>
        <button type="button" className={ui.btn} onClick={() => void act(() => setScheduleEnabled(v.id, !v.enabled))}>
          {v.enabled ? "Pause" : "Resume"}
        </button>
      </td>
    </tr>
  );
}

// ProposalRow is a schedule the director proposed: accept it as it is, edit
// it before accepting, or refuse it with a reason.
function ProposalRow({ proposal: p, onEdit }: { proposal: ScheduleProposal; onEdit: () => void }) {
  const [refusing, setRefusing] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const s = p.schedule;

  async function act(action: () => Promise<unknown>) {
    setBusy(true);
    try {
      await action();
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <article className={styles.proposal}>
      <div>
        <strong>{s.title}</strong>
        <span className={styles.rowSub}>
          {s.id} · {recurrenceText(s)} · {s.target} · {s.mode} · {p.id} from {p.proposed_by}
        </span>
        <p className={styles.instructions}>{s.instructions}</p>
        {p.warning && <p className={styles.warning}>{p.warning}</p>}
        {error && <p className={ui.error}>{error}</p>}
      </div>
      {refusing ? (
        <div className={styles.actions}>
          <input className={ui.input} placeholder="Reason, passed on to the director" aria-label="Reason for refusing" value={reason} onChange={(e) => setReason(e.target.value)} />
          <button type="button" className={ui.btn} disabled={busy} onClick={() => void act(() => refuseScheduleProposal(p.id, reason))}>
            Refuse
          </button>
          <button type="button" className={ui.link} onClick={() => setRefusing(false)}>
            Cancel
          </button>
        </div>
      ) : (
        <div className={styles.actions}>
          <button type="button" className={ui.btnPrimary} disabled={busy} onClick={() => void act(() => acceptScheduleProposal(p.id))}>
            Accept
          </button>
          <button type="button" className={ui.btn} disabled={busy} onClick={onEdit}>
            Edit…
          </button>
          <button type="button" className={ui.btn} disabled={busy} onClick={() => setRefusing(true)}>
            Refuse…
          </button>
        </div>
      )}
    </article>
  );
}

// FormDrawer holds a form on the right side, like a schedule's drawer.
function FormDrawer({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <aside className={kanban.drawer} role="dialog" aria-label={title}>
      <div className={kanban.drawerBody}>
        <div className={kanban.drawerHead}>
          <h3 className={ui.title}>{title}</h3>
          <button type="button" className={ui.link} onClick={onClose}>
            Close
          </button>
        </div>
        {children}
      </div>
    </aside>
  );
}
