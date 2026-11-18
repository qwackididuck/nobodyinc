"use client";

import { type DragEvent, useState } from "react";

import { setTicketStatus, type Ticket, type TicketStatus } from "@/lib/api";
import { useLive } from "@/lib/live";
import { lookOf, teamColors } from "@/lib/look";
import { Spent } from "../budgets/Spent";
import { isDirector } from "../chat/agents";
import { Sprite } from "../office/sprite";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";
import { NewTicket } from "./NewTicket";
import { Shredder } from "./Shredder";
import { TicketDrawer } from "./TicketDrawer";

const COLUMNS: { status: TicketStatus; label: string }[] = [
  { status: "open", label: "Open" },
  { status: "in_progress", label: "In progress" },
  { status: "done", label: "Done" },
  { status: "canceled", label: "Canceled" },
];

interface Props {
  onOpenChat: (address: string) => void;
}

// Kanban shows the tickets as index cards pinned on a cork board, one column
// per status. Dragging a card to another column changes its status (the
// human may change any ticket), dragging it onto the shredder deletes it;
// clicking it opens its details. Tickets of finished features are archived
// and not shown here.
export function Kanban({ onOpenChat }: Props) {
  const { tickets, teams, features, agents, approvals } = useLive();
  const [team, setTeam] = useState<string | null>(null);
  const [onlyActive, setOnlyActive] = useState(true);
  const [open, setOpen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [over, setOver] = useState<TicketStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  const active = features.find((f) => f.active);
  const teamIndex = new Map(teams.map((t, i) => [t.id, i]));
  const byAddress = new Map(agents.map((a) => [a.address, a]));
  const waiting = new Set(approvals.map((a) => a.agent));

  const shown = tickets.filter((t) => (!team || t.team === team) && (!onlyActive || !active || t.feature === active.id));

  async function drop(e: DragEvent, status: TicketStatus) {
    e.preventDefault();
    setOver(null);
    const id = e.dataTransfer.getData("text/plain");
    const ticket = tickets.find((t) => t.id === id);
    if (!ticket || ticket.status === status) return;
    try {
      await setTicketStatus(id, status);
      setError(null);
    } catch (err) {
      setError(`${id}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  function card(t: Ticket, i: number) {
    const colors = teamColors(teamIndex.get(t.team) ?? 0);
    const assignee = t.assignee ? byAddress.get(t.assignee) : undefined;
    const criteria = (t.acceptance_criteria ?? "").split("\n").filter((l) => l.trim()).length;
    return (
      <li
        key={t.id}
        className={styles.ticket}
        data-status={t.status}
        draggable
        style={{ ["--team" as string]: colors.ink, ["--tilt" as string]: `${((i % 3) - 1) * 1.2}deg` }}
        onDragStart={(e) => e.dataTransfer.setData("text/plain", t.id)}
      >
        <button type="button" className={styles.ticketOpen} onClick={() => setOpen(t.id)} aria-label={`Open ${t.id}: ${t.title}`} />
        <div className={styles.top}>
          <span className={styles.id}>{t.id}</span>
          <span className={styles.team}>{t.team}</span>
        </div>
        <p className={styles.ticketTitle}>{t.title}</p>
        <div className={styles.meta}>
          {criteria > 0 && <span>{criteria} acceptance criteria</span>}
          {t.blocked_by?.length ? <span>needs {t.blocked_by.join(", ")}</span> : null}
          {(t.budget_usd !== undefined || t.spent_usd > 0) && <Spent spent={t.spent_usd} budget={t.budget_usd} threshold={t.threshold} />}
        </div>
        <div className={styles.foot}>
          {t.assignee ? (
            <button type="button" className={styles.who} title={`Open ${t.assignee}'s chat`} onClick={() => onOpenChat(t.assignee as string)}>
              <span className={ui.mini}>
                <Sprite look={lookOf(t.assignee, assignee ? isDirector(assignee) : false)} />
              </span>
              {t.assignee.split("@")[0]}
            </button>
          ) : (
            <span className={styles.queue}>In the {t.team} queue</span>
          )}
          {t.assignee && waiting.has(t.assignee) && <span className={styles.flag}>APPROVAL</span>}
          {assignee?.status === "budget_hold" && <span className={styles.flag}>BUDGET HOLD</span>}
        </div>
      </li>
    );
  }

  return (
    // The frame does not scroll: the shredder and the drawers stay put while
    // the board scrolls under them.
    <div className={styles.frame}>
      <div className={styles.board}>
        <div className={styles.bar}>
          <h2 className={`${ui.title} ${styles.boardTitle}`}>Tickets</h2>
          {active && (
            <button type="button" className={`${ui.chip} ${onlyActive ? ui.chipOn : ""}`} aria-pressed={onlyActive} onClick={() => setOnlyActive(!onlyActive)}>
              {onlyActive ? `Feature: ${active.title}` : "All features"}
            </button>
          )}
          <span className={styles.sep} />
          {[null, ...teams.map((t) => t.id)].map((id) => (
            <button key={id ?? "all"} type="button" className={`${ui.chip} ${team === id ? ui.chipOn : ""}`} aria-pressed={team === id} onClick={() => setTeam(id)}>
              {id ?? "All teams"}
            </button>
          ))}
          <span className={styles.spacer} />
          <button type="button" className={ui.btnPrimary} onClick={() => setCreating(true)}>
            + New ticket
          </button>
        </div>
        {error && (
          <p className={`${ui.error} ${styles.boardError}`}>
            {error}{" "}
            <button type="button" className={ui.link} onClick={() => setError(null)}>
              Dismiss
            </button>
          </p>
        )}
        <div className={styles.columns}>
          {COLUMNS.map((c) => {
            const items = shown.filter((t) => t.status === c.status);
            return (
              <section
                key={c.status}
                className={styles.column}
                data-status={c.status}
                data-over={over === c.status}
                aria-label={c.label}
                onDragOver={(e) => {
                  e.preventDefault();
                  setOver(c.status);
                }}
                onDragLeave={() => setOver(null)}
                onDrop={(e) => void drop(e, c.status)}
              >
                <h3 className={styles.columnHead}>
                  {c.label} <b>{items.length}</b>
                </h3>
                <ol className={styles.cards}>{items.map(card)}</ol>
              </section>
            );
          })}
        </div>
      </div>
      <Shredder tickets={tickets} onShredded={(ids) => ids.includes(open ?? "") && setOpen(null)} />
      {open && <TicketDrawer id={open} onClose={() => setOpen(null)} onOpenChat={onOpenChat} />}
      {creating && <NewTicket onClose={() => setCreating(false)} onCreated={(id) => setOpen(id)} />}
    </div>
  );
}
