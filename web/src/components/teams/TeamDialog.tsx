"use client";

import { type FormEvent, type ReactNode, useState } from "react";

import { createTeam, deleteTeam, type Team, updateTeam } from "@/lib/api";
import { useLive } from "@/lib/live";
import { CapEditor } from "../budgets/CapEditor";
import { NAME_PATTERN } from "../staff/definition";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./teams.module.css";

interface Props {
  // The team to edit; absent to create one.
  team?: Team;
  onClose: () => void;
  // Called with the team once created or saved.
  onDone?: (team: Team) => void;
}

// TeamDialog creates a team (a new room of the office: a name, a goal, the
// instructions its members share and, under advanced settings, the teams its
// members may start conversations with), or edits them, or deletes the team. Its members
// restart with a fresh session once idle when the goal or prompt changes
// (docs/agents.md, "The prompt cascade"); a change of flows only sends them a
// notice (ADR-0009); deleting the team dismisses them.
export function TeamDialog({ team, onClose, onDone }: Props) {
  const editing = Boolean(team);
  const [id, setID] = useState(team?.id ?? "");
  const [goal, setGoal] = useState(team?.goal ?? "");
  const [prompt, setPrompt] = useState(team?.prompt ?? "");
  const [block, setBlock] = useState<string[]>(team?.block ?? []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    // The dialog may be opened from another form (recruiting): its submit
    // must not reach that form through the React tree.
    e.stopPropagation();
    setBusy(true);
    try {
      const saved = team ? await updateTeam(team.id, { goal, prompt, block }) : await createTeam({ id: id.trim(), goal, prompt, block });
      onDone?.(saved);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal label={editing ? `Edit team ${team?.id}` : "New team"} onClose={onClose}>
      <form className={styles.form} onSubmit={(e) => void submit(e)}>
        <h3 className={ui.title}>{editing ? `Team ${team?.id}` : "New team"}</h3>
        <p className={ui.sub}>
          {editing
            ? "A new goal or new instructions restart its members with a new session once their turn is over. A change of flows does not: they are told."
            : "A new room in the office. Recruit its members afterwards, from the catalog or by asking the director."}
        </p>
        {!editing && (
          <label className={ui.label}>
            NAME
            <input
              id="team-id"
              className={ui.input}
              placeholder="e.g. backend"
              value={id}
              onChange={(e) => setID(e.target.value)}
              pattern={NAME_PATTERN}
              title="Lowercase letters, digits and dashes: it is part of the members' addresses (name@team)."
              required
              autoFocus
            />
            <span className={styles.hint}>Part of its members&apos; addresses: {id.trim() ? `alice@${id.trim()}` : "alice@backend"}.</span>
          </label>
        )}
        <label className={ui.label}>
          GOAL
          <input
            id="team-goal"
            className={ui.input}
            placeholder="e.g. Build the API with its tests"
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
            autoFocus={editing}
          />
          <span className={styles.hint}>One sentence: what the team is for. Every member and colleague sees it.</span>
        </label>
        <label className={ui.label}>
          INSTRUCTIONS SHARED BY THE TEAM (OPTIONAL)
          <textarea
            id="team-prompt"
            className={`${ui.textarea} ${styles.prompt}`}
            placeholder={"How the team works: the order of its work, who hands over to whom, its conventions.\nEmpty: the goal alone."}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          <span className={styles.hint}>Saved in .nobody/teams/{(team?.id ?? id.trim()) || "<team>"}/TEAM.md, after the project prompt in each member&apos;s instructions.</span>
        </label>
        <Advanced blocked={block.length}>
          <Flows team={team?.id ?? id.trim()} block={block} onBlock={setBlock} />
        </Advanced>
        {error && <p className={ui.error}>{error}</p>}
        <div className={styles.actions}>
          <button type="submit" className={ui.btnPrimary} disabled={busy || (!editing && !id.trim())}>
            {editing ? "Save" : "Build the room"}
          </button>
          <button type="button" className={ui.link} onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
      {team && <CapEditor scope={{ kind: "team", id: team.id }} title={`Cap on team ${team.id}`} />}
      {team && <DeleteTeam team={team} onDeleted={onClose} />}
    </Modal>
  );
}

// Advanced folds the settings most teams never change (today the flows),
// closed by default; its summary says how many teams are blocked.
function Advanced({ blocked, children }: { blocked: number; children: ReactNode }) {
  return (
    <details className={styles.advanced}>
      <summary>
        Advanced settings
        {blocked > 0 && <span className={styles.hint}> · {blocked === 1 ? "1 team blocked" : `${blocked} teams blocked`}</span>}
      </summary>
      <div className={styles.advancedBody}>{children}</div>
    </details>
  );
}

// Flows lists the other teams, each with a box: checked, the team's members
// may start conversations with it (the default); unchecked, it is blocked
// and they can only answer its members (ADR-0009).
function Flows({ team, block, onBlock }: { team: string; block: string[]; onBlock: (block: string[]) => void }) {
  const { teams } = useLive();
  const others = teams.filter((t) => t.id !== team);

  if (others.length === 0) {
    return <p className={styles.hint}>No other team yet: every flow is open. Once there are others, choose here whom this team may start conversations with.</p>;
  }

  function toggle(other: string, open: boolean) {
    onBlock(open ? block.filter((b) => b !== other) : [...block, other]);
  }

  return (
    <fieldset className={styles.flows}>
      <legend className={ui.label}>CAN START CONVERSATIONS WITH</legend>
      {others.map((t) => (
        <label key={t.id} className={styles.flow}>
          <input type="checkbox" checked={!block.includes(t.id)} onChange={(e) => toggle(t.id, e.target.checked)} />
          <span>
            <strong>{t.id}</strong>
            {t.goal && <span className={styles.hint}> · {t.goal}</span>}
          </span>
        </label>
      ))}
      <span className={styles.hint}>
        Unchecked: its members cannot start a conversation with that team, or write to @team, but can still answer it. Tickets, the human and the
        director are never blocked.
      </span>
    </fieldset>
  );
}

interface DeleteTeamProps {
  team: Team;
  onDeleted: () => void;
  // When set, the confirmation shows at once and Cancel calls it (the Staff
  // list's "Delete team", in its own dialog); otherwise a link opens it.
  onCancel?: () => void;
}

// DeleteTeam deletes a team after a confirmation that says who is dismissed
// and what happens to the tickets: the team dialog's danger zone, or the
// dialog opened by "Delete team" in the Staff list.
export function DeleteTeam({ team, onDeleted, onCancel }: DeleteTeamProps) {
  const { agents, tickets } = useLive();
  const [confirming, setConfirming] = useState(Boolean(onCancel));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const members = agents.filter((a) => a.team === team.id);
  const live = tickets.filter((t) => t.team === team.id && !t.archived);
  const inProgress = live.filter((t) => t.status === "in_progress");
  const open = live.filter((t) => t.status === "open");

  async function remove() {
    setBusy(true);
    try {
      await deleteTeam(team.id);
      onDeleted();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  }

  if (!confirming) {
    return (
      <div className={styles.danger}>
        <button type="button" className={ui.link} onClick={() => setConfirming(true)}>
          Delete this team…
        </button>
      </div>
    );
  }

  return (
    <div className={`${styles.danger} ${styles.confirm}`}>
      <strong>Delete team {team.id}?</strong>
      <span>Its room is taken down, and .nobody/teams/{team.id}/ is deleted.</span>
      {members.length > 0 ? (
        <span>
          {members.length === 1 ? "Its member is dismissed" : `Its ${members.length} members are dismissed`}: {members.map((a) => a.name).join(", ")}. Each one
          stops once its turn is over; undelivered mail goes back to the senders.
        </span>
      ) : (
        <span>It has no member.</span>
      )}
      {inProgress.length > 0 && (
        <span className={styles.warning}>
          {inProgress.length === 1 ? "1 ticket is" : `${inProgress.length} tickets are`} in progress ({inProgress.map((t) => t.id).join(", ")}): the work stops,
          and {inProgress.length === 1 ? "it goes" : "they go"} back to Open. Uncommitted changes are lost.
        </span>
      )}
      {open.length + inProgress.length > 0 && (
        <span>
          Its tickets stay on the kanban ({open.length + inProgress.length} open after this): delete them with the shredder, or keep them.
        </span>
      )}
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.actions}>
        <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void remove()}>
          Delete {team.id}
          {members.length > 0 ? ` and dismiss ${members.length}` : ""}
        </button>
        <button type="button" className={ui.link} onClick={() => (onCancel ? onCancel() : setConfirming(false))}>
          Cancel
        </button>
      </div>
    </div>
  );
}
