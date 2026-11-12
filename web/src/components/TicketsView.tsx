"use client";

import { type FormEvent, useCallback, useState } from "react";

import {
  type Ticket,
  type TicketStatus,
  commentTicket,
  createTicket,
  getTicket,
  listTickets,
  setTicketStatus,
} from "@/lib/api";
import { usePolling } from "@/lib/usePolling";
import styles from "./ControlRoom.module.css";

const REFRESH_MS = 2000;

interface Props {
  teams: string[];
}

// TicketsView lists the tickets, lets the human create one, and shows the
// selected ticket with its deliverables, comments and messages. Deliberately
// plain: the real design pass comes later.
export function TicketsView({ teams }: Props) {
  const tickets = usePolling(listTickets, REFRESH_MS);
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <div className={styles.tickets}>
      <NewTicketForm teams={teams} onCreated={(t) => { setSelected(t.id); void tickets.refresh(); }} />
      {tickets.error && <p className={styles.error}>{tickets.error}</p>}
      <table className={styles.ticketTable}>
        <thead>
          <tr>
            <th>ID</th>
            <th>Title</th>
            <th>Team</th>
            <th>Assignee</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {(tickets.data ?? []).map((t) => (
            <tr
              key={t.id}
              className={t.id === selected ? styles.ticketRowSelected : styles.ticketRow}
              onClick={() => setSelected(t.id)}
            >
              <td>{t.id}</td>
              <td>
                {t.title}
                {t.blocked_by && t.blocked_by.length > 0 && <span className={styles.muted}> (after {t.blocked_by.join(", ")})</span>}
              </td>
              <td>{t.team}</td>
              <td>{t.assignee || "—"}</td>
              <td>{t.status}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {selected && <TicketDetail key={selected} id={selected} />}
    </div>
  );
}

function NewTicketForm({ teams, onCreated }: { teams: string[]; onCreated: (t: Ticket) => void }) {
  const [title, setTitle] = useState("");
  const [team, setTeam] = useState("");
  const [description, setDescription] = useState("");
  const [criteria, setCriteria] = useState("");
  const [blockedBy, setBlockedBy] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const t = await createTicket({
        title,
        team: team || teams[0] || "",
        description,
        acceptance_criteria: criteria,
        blocked_by: blockedBy.split(/[\s,]+/).filter(Boolean),
      });
      setTitle("");
      setDescription("");
      setCriteria("");
      setBlockedBy("");
      setError(null);
      onCreated(t);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <form className={styles.composer} onSubmit={submit}>
      <div className={styles.row}>
        <input className={styles.grow} placeholder="New ticket title" value={title} onChange={(e) => setTitle(e.target.value)} />
        <select value={team || teams[0] || ""} onChange={(e) => setTeam(e.target.value)} aria-label="Team">
          {teams.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
        <input placeholder="Blocked by (T-1, T-2)" value={blockedBy} onChange={(e) => setBlockedBy(e.target.value)} />
      </div>
      <textarea placeholder="Description" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
      <textarea placeholder="Acceptance criteria" rows={2} value={criteria} onChange={(e) => setCriteria(e.target.value)} />
      <div className={styles.row}>
        <button type="submit" className={styles.primaryButton} disabled={!title}>
          Create ticket
        </button>
        {error && <span className={styles.error}>{error}</span>}
      </div>
    </form>
  );
}

function TicketDetail({ id }: { id: string }) {
  const load = useCallback(() => getTicket(id), [id]);
  const details = usePolling(load, REFRESH_MS);
  const [comment, setComment] = useState("");
  const [error, setError] = useState<string | null>(null);

  if (!details.data) return details.error ? <p className={styles.error}>{details.error}</p> : null;
  const { ticket, deliverables, comments, messages } = details.data;
  const closed = ticket.status === "done" || ticket.status === "canceled";

  async function act(action: () => Promise<unknown>) {
    try {
      await action();
      setError(null);
      void details.refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  const changeStatus = (status: TicketStatus) => act(() => setTicketStatus(id, status));

  return (
    <article className={styles.ticketDetail}>
      <h3 className={styles.subject}>
        {ticket.id} — {ticket.title}
      </h3>
      <p className={styles.muted}>
        {ticket.team} · {ticket.status} · {ticket.assignee ? `assigned to ${ticket.assignee}` : "in the team's queue"} · created by{" "}
        {ticket.created_by}
      </p>
      {ticket.description && <p className={styles.body}>{ticket.description}</p>}
      {ticket.acceptance_criteria && (
        <p className={styles.body}>
          <strong>Acceptance criteria:</strong> {ticket.acceptance_criteria}
        </p>
      )}
      {!closed && (
        <div className={styles.row}>
          <button type="button" className={styles.smallButton} onClick={() => changeStatus("done")}>
            Mark done
          </button>
          {ticket.assignee && (
            <button type="button" className={styles.smallButton} onClick={() => changeStatus("open")}>
              Back to queue
            </button>
          )}
          <button type="button" className={styles.denyButton} onClick={() => changeStatus("canceled")}>
            Cancel
          </button>
        </div>
      )}

      <h4 className={styles.panelTitle}>Deliverables</h4>
      {deliverables.length === 0 && <p className={styles.empty}>None yet.</p>}
      <ul className={styles.plainList}>
        {deliverables.map((d) => (
          <li key={d.id}>
            <strong>{d.kind}</strong> {d.reference && <code>{d.reference}</code>} — {d.summary} <span className={styles.muted}>({d.author})</span>
          </li>
        ))}
      </ul>

      <h4 className={styles.panelTitle}>Comments</h4>
      <ul className={styles.plainList}>
        {comments.map((c) => (
          <li key={c.id}>
            <strong>{c.author}</strong>: {c.body}
          </li>
        ))}
      </ul>
      <form
        className={styles.row}
        onSubmit={(e) => {
          e.preventDefault();
          void act(async () => {
            await commentTicket(id, comment);
            setComment("");
          });
        }}
      >
        <input className={styles.grow} placeholder="Comment" value={comment} onChange={(e) => setComment(e.target.value)} />
        <button type="submit" className={styles.smallButton} disabled={!comment}>
          Comment
        </button>
      </form>

      <h4 className={styles.panelTitle}>Messages about this ticket</h4>
      {messages.length === 0 && <p className={styles.empty}>None.</p>}
      <ul className={styles.plainList}>
        {messages.map((m) => (
          <li key={m.id}>
            <strong>
              {m.from} → {m.to}
            </strong>{" "}
            [{m.act}] {m.subject}
          </li>
        ))}
      </ul>
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}
