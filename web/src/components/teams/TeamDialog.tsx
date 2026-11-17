"use client";

import { type FormEvent, useState } from "react";

import { createTeam, type Team, updateTeam } from "@/lib/api";
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
// the instructions its members share) or edits one's goal and prompt. Its
// members restart with a fresh session once idle when the goal or prompt
// changes (docs/agents.md, "The prompt cascade").
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
    </Modal>
  );
}
