"use client";

import { type FormEvent, useEffect, useState } from "react";

import { commentTicket, getTicket, setTicketStatus, type Ticket, type TicketDetails, type TicketStatus, updateTicketBudget } from "@/lib/api";
import { useDaemonEvents } from "@/lib/live";
import { parseUSD, useBudgetsOn } from "../budgets/CapEditor";
import { Spent } from "../budgets/Spent";
import { timeOf } from "../chat/agents";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";

const STATUSES: { status: TicketStatus; label: string }[] = [
  { status: "open", label: "Open" },
  { status: "in_progress", label: "In progress" },
  { status: "done", label: "Done" },
  { status: "canceled", label: "Canceled" },
];

interface Props {
  id: string;
  onClose: () => void;
  onOpenChat: (address: string) => void;
  // Archived tickets are shown read-only.
  readOnly?: boolean;
}

// TicketDrawer shows everything about a ticket: the need and its acceptance
// criteria, the deliverables, the comments and the messages attached to it.
// The human may change its status and comment.
export function TicketDrawer({ id, onClose, onOpenChat, readOnly = false }: Props) {
  const [details, setDetails] = useState<TicketDetails | null>(null);
  const [comment, setComment] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    getTicket(id)
      .then((d) => !cancelled && setDetails(d))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, [id, version]);

  // Read it again when it changes (status, comment, deliverable), and as it
  // spends.
  useDaemonEvents((e) => {
    if ((e.type === "tickets.changed" && e.data.id === id) || e.type === "spend.changed") setVersion((v) => v + 1);
  });

  async function act(action: () => Promise<unknown>) {
    try {
      await action();
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!comment.trim()) return;
    void act(async () => {
      await commentTicket(id, comment.trim());
      setComment("");
    });
  }

  const t = details?.ticket;

  return (
    <aside className={styles.drawer} role="dialog" aria-label={`Ticket ${id}`}>
      <div className={styles.drawerBody}>
        <div className={styles.drawerHead}>
          <span className={styles.id}>{id}</span>
          <button type="button" className={ui.link} onClick={onClose}>
            Close
          </button>
        </div>
        {!t && !error && <p className={ui.sub}>Loading…</p>}
        {t && (
          <>
            <h3 className={ui.title}>{t.title}</h3>
            <p className={ui.sub}>
              Team {t.team}
              {t.assignee && (
                <>
                  {" · "}
                  <button type="button" className={ui.link} onClick={() => onOpenChat(t.assignee as string)}>
                    {t.assignee}
                  </button>
                </>
              )}
              {t.feature && ` · ${t.feature}`} · created by {t.created_by}
            </p>
            {readOnly ? (
              <span className={ui.chip}>
                {STATUSES.find((s) => s.status === t.status)?.label}
                {t.archived ? " · archived" : ""}
              </span>
            ) : (
              <div className={ui.seg} role="group" aria-label="Status">
                {STATUSES.map((s) => (
                  <button key={s.status} type="button" aria-pressed={t.status === s.status} onClick={() => void act(() => setTicketStatus(id, s.status))}>
                    {s.label}
                  </button>
                ))}
              </div>
            )}
            {t.blocked_by?.length ? <p className={ui.sub}>Waits for {t.blocked_by.join(", ")}</p> : null}
            <TicketBudget ticket={t} readOnly={readOnly} onChange={(b) => act(() => updateTicketBudget(id, b))} />
            {t.description && <Section title="What is needed">{t.description}</Section>}
            {t.acceptance_criteria && <Section title="Acceptance criteria">{t.acceptance_criteria}</Section>}
            <h4 className={styles.sectionTitle}>Deliverables</h4>
            {details.deliverables.length === 0 && <p className={ui.sub}>None yet.</p>}
            <ul className={styles.list}>
              {details.deliverables.map((d) => (
                <li key={d.id}>
                  <span className={ui.chip}>{d.kind}</span> {d.summary}
                  {d.reference && <code className={styles.ref}>{d.reference}</code>}
                  <span className={styles.by}>
                    {d.author} · {timeOf(d.created_at)}
                  </span>
                </li>
              ))}
            </ul>
            <h4 className={styles.sectionTitle}>Comments</h4>
            <ul className={styles.list}>
              {details.comments.map((c) => (
                <li key={c.id}>
                  <span className={styles.by}>
                    {c.author} · {timeOf(c.created_at)}
                  </span>
                  <p className={styles.text}>{c.body}</p>
                </li>
              ))}
            </ul>
            {!readOnly && <form className={styles.commentForm} onSubmit={submit}>
              <input
                id="ticket-comment"
                className={ui.input}
                placeholder="Comment as the human"
                aria-label="Comment"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
              <button type="submit" className={ui.btn} disabled={!comment.trim()}>
                Comment
              </button>
            </form>}
            {details.messages.length > 0 && (
              <>
                <h4 className={styles.sectionTitle}>Messages about it</h4>
                <ul className={styles.list}>
                  {details.messages.map((m) => (
                    <li key={m.id}>
                      <span className={styles.by}>
                        {m.from} → {m.to} · {m.act} · {timeOf(m.sent_at)}
                      </span>
                      <p className={styles.text}>{m.subject || m.body}</p>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </>
        )}
        {error && <p className={ui.error}>{error}</p>}
      </div>
    </aside>
  );
}

interface TicketBudgetProps {
  ticket: Ticket;
  readOnly: boolean;
  onChange: (budget: { budget_usd: number | null }) => Promise<void>;
}

// TicketBudget shows what the ticket spent against its budget, and lets the
// human change the budget (ADR-0013; inside a feature with a budget, at most
// what the feature has left).
function TicketBudget({ ticket, readOnly, onChange }: TicketBudgetProps) {
  const budgetsOn = useBudgetsOn();
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(ticket.budget_usd !== undefined ? String(ticket.budget_usd) : "");
  const amount = parseUSD(value);

  if (!budgetsOn && ticket.spent_usd === 0) return null;

  const editable = budgetsOn && !readOnly && ticket.status !== "canceled";

  return (
    <p className={ui.sub}>
      Budget: <Spent spent={ticket.spent_usd} budget={ticket.budget_usd} threshold={ticket.threshold} />
      {budgetsOn && ticket.budget_usd === undefined && " (none)"}
      {editable && !editing && (
        <>
          {" · "}
          <button type="button" className={ui.link} onClick={() => setEditing(true)}>
            Change
          </button>
        </>
      )}
      {editing && (
        <>
          {" "}
          <input
            id="ticket-budget-edit"
            className={ui.input}
            inputMode="decimal"
            aria-label="Budget in USD"
            placeholder="none"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />{" "}
          <button
            type="button"
            className={ui.btn}
            disabled={Number.isNaN(amount)}
            onClick={() =>
              void onChange({ budget_usd: amount ?? null }).then(() => {
                setEditing(false);
              })
            }
          >
            Save
          </button>{" "}
          <button type="button" className={ui.link} onClick={() => setEditing(false)}>
            Cancel
          </button>
        </>
      )}
    </p>
  );
}

function Section({ title, children }: { title: string; children: string }) {
  return (
    <>
      <h4 className={styles.sectionTitle}>{title}</h4>
      <p className={styles.text}>{children}</p>
    </>
  );
}
