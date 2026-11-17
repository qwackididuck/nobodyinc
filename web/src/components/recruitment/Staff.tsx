"use client";

import { useState } from "react";

import { type Agent, dismissAgent, putOnLeave, returnFromLeave, type Team } from "@/lib/api";
import { useLive } from "@/lib/live";
import { lookOf } from "@/lib/look";
import { isDirector, sortAgents, statusKey, statusLabel } from "../chat/agents";
import { Sprite } from "../office/sprite";
import { TeamDialog } from "../teams/TeamDialog";
import ui from "../ui/ui.module.css";
import styles from "./recruitment.module.css";

interface Props {
  onOpenChat: (address: string) => void;
  onEdit: (address: string) => void;
}

// Staff lists everyone in the office, team by team, with what can be done
// to them: open their chat, edit their file, put them on leave or bring them
// back, dismiss them. A dismissed agent walks out of the office. The director
// can only be edited. Teams can be created here, and their goal and shared
// instructions edited.
export function Staff({ onOpenChat, onEdit }: Props) {
  const { agents, teams } = useLive();
  const [now, setNow] = useState(false);
  // The team dialog: null closed, "new" to create one, or the team to edit.
  const [dialog, setDialog] = useState<Team | "new" | null>(null);
  const sorted = sortAgents(agents, teams);
  const director = sorted.filter(isDirector);
  const groups: { team?: Team; label: string; members: Agent[] }[] = [
    ...(director.length > 0 ? [{ label: "Director", members: director }] : []),
    ...teams.map((t) => ({ team: t, label: t.id, members: sorted.filter((a) => a.team === t.id) })),
  ];

  return (
    <div className={styles.staff}>
      <div className={styles.staffBar}>
        <label className={styles.nowToggle}>
          <button type="button" role="switch" className={ui.switch} aria-checked={now} aria-label="Apply at once" onClick={() => setNow(!now)} />
          Apply at once, without waiting for the end of the current turn
        </label>
        <button type="button" className={ui.btn} onClick={() => setDialog("new")}>
          + New team
        </button>
      </div>
      {groups.map((g) => (
        <section key={g.label} className={styles.staffTeam}>
          <header className={styles.staffTeamHead}>
            <h3>
              {g.label.toUpperCase()} · {g.members.length}
            </h3>
            {g.team?.goal && <span className={styles.teamGoal}>{g.team.goal}</span>}
            {g.team && (
              <button type="button" className={ui.link} onClick={() => setDialog(g.team ?? null)}>
                Edit team
              </button>
            )}
          </header>
          {g.members.map((a) => (
            <Member key={a.address} agent={a} now={now} onOpenChat={onOpenChat} onEdit={onEdit} />
          ))}
          {g.members.length === 0 && <p className={ui.sub}>No member yet: recruit one from the catalog.</p>}
        </section>
      ))}
      {teams.length === 0 && <p className={ui.sub}>No team yet: create one, then recruit its members from the catalog.</p>}
      {dialog && <TeamDialog team={dialog === "new" ? undefined : dialog} onClose={() => setDialog(null)} />}
    </div>
  );
}

function Member({ agent: a, now, onOpenChat, onEdit }: { agent: Agent; now: boolean; onOpenChat: (address: string) => void; onEdit: (address: string) => void }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const when = now ? "now" : "once its turn is over";
  // The director is always present: it can be edited, never sent away.
  const director = isDirector(a);

  async function act(action: () => Promise<unknown>, done: string) {
    setBusy(true);
    try {
      await action();
      setError(null);
      setNote(done);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  }

  return (
    <div className={styles.member}>
      <span className={ui.mini}>
        <Sprite look={lookOf(a.address, isDirector(a))} />
      </span>
      <div className={styles.memberWho}>
        <strong>{a.name}</strong>
        <span title={a.expertise ? `Ask about: ${a.expertise}` : undefined}>
          {a.role || "Agent"}
          {a.pending_change && " · change waiting for the end of its turn"}
        </span>
      </div>
      <span className={styles.pill} data-status={statusKey(a)}>
        {statusLabel(a)}
      </span>
      <span className={styles.from} title="Recruited from this template (information only)">
        {a.from ? `from ${a.from}` : "written from scratch"}
      </span>
      <div className={styles.memberActions}>
        <button type="button" className={ui.btn} onClick={() => onOpenChat(a.address)}>
          Chat
        </button>
        <button type="button" className={ui.btn} onClick={() => onEdit(a.address)}>
          Edit
        </button>
        {a.on_leave ? (
          <button type="button" className={ui.btn} disabled={busy} onClick={() => void act(() => returnFromLeave(a.address), `${a.name} is back.`)}>
            Return
          </button>
        ) : (
          !director && (
            <button type="button" className={ui.btn} disabled={busy} onClick={() => void act(() => putOnLeave(a.address, { now }), `${a.name} goes on leave ${when}.`)}>
              Leave
            </button>
          )
        )}
        {!director && (
          <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => setConfirming(true)}>
            Dismiss
          </button>
        )}
      </div>
      {confirming && (
        <div className={styles.confirm}>
          <span>
            Dismiss {a.name}? Its file is deleted at once; its ticket in progress goes back to the queue and its undelivered mail back to the senders. History is kept.
          </span>
          <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void act(() => dismissAgent(a.address, { now }), `${a.name} is leaving ${when}.`)}>
            Dismiss {a.name}
          </button>
          <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
            Cancel
          </button>
        </div>
      )}
      {(note || error) && <p className={`${error ? ui.error : ui.note} ${styles.memberNote}`}>{error ?? note}</p>}
    </div>
  );
}
