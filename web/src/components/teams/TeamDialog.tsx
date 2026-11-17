"use client";

import { type FormEvent, useState } from "react";

import { createTeam, deleteTeam, type Team, updateTeam } from "@/lib/api";
import { useLive } from "@/lib/live";
import { NAME_PATTERN } from "../recruitment/definition";
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

// TeamDialog creates a team (a new room of the office: a name, a goal, and
// the instructions its members share), or edits one's goal and prompt, or
// deletes it. Its members restart with a fresh session once idle when the
// goal or prompt changes (docs/agents.md, "The prompt cascade"); deleting the
// team dismisses them.
export function TeamDialog({ team, onClose, onDone }: Props) {
  const editing = Boolean(team);
  const [id, setID] = useState(team?.id ?? "");
  const [goal, setGoal] = useState(team?.goal ?? "");
  const [prompt, setPrompt] = useState(team?.prompt ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    // The dialog may be opened from another form (recruiting): its submit
    // must not reach that form through the React tree.
    e.stopPropagation();
    setBusy(true);
    try {
      const saved = team ? await updateTeam(team.id, { goal, prompt }) : await createTeam({ id: id.trim(), goal, prompt });
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
            ? "Its members restart with a new session once their turn is over, to read the new goal and instructions."
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
      {team && <DeleteTeam team={team} onDeleted={onClose} />}
    </Modal>
  );
}

// DeleteTeam is the dialog's danger zone: deleting the team, after a
// confirmation that says who is dismissed and what happens to the tickets.
function DeleteTeam({ team, onDeleted }: { team: Team; onDeleted: () => void }) {
  const { agents, tickets } = useLive();
  const [confirming, setConfirming] = useState(false);
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
        <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
          Cancel
        </button>
      </div>
    </div>
  );
}
