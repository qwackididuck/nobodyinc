"use client";

import { type ReactNode, useState } from "react";

import { deleteProject, DeleteRefused, type UnfinishedFeature } from "@/lib/launcher";
import { windowID } from "@/lib/window";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./projects.module.css";

interface Props {
  project: { id: string; name: string };
  onClose: () => void;
  onDeleted: () => void;
}

type Choice = "merge" | "discard";

// DeleteProject removes Nobody Inc from a project, never its code (ADR-0014).
// It says what goes and what stays, and asks for the project's name. When a
// feature is not finished, the launcher refuses and nothing is deleted: the
// human then merges it as it is, or deletes anyway.
export function DeleteProject({ project, onClose, onDeleted }: Props) {
  const [typed, setTyped] = useState("");
  const [feature, setFeature] = useState<UnfinishedFeature | null>(null);
  const [choice, setChoice] = useState<Choice>("merge");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function remove(withChoice?: Choice) {
    setBusy(true);
    setError(null);
    try {
      await deleteProject(project.id, windowID(), withChoice);
      onDeleted();
    } catch (e) {
      if (e instanceof DeleteRefused) {
        // The first refusal asks; a refused merge says why.
        if (feature) setError(e.message);
        setFeature(e.feature);
      } else {
        setError((e as Error).message);
      }
      setBusy(false);
    }
  }

  if (feature) {
    return (
      <Modal label="Unfinished feature" tone="danger" onClose={onClose}>
        <h3 className={ui.title}>A feature is not finished</h3>
        <div className={styles.featureBox}>
          Feature <b>{feature.title}</b> on <code>{feature.branch}</code>
        </div>
        <div className={styles.options} role="radiogroup" aria-label="What to do with the feature">
          <ChoiceButton value="merge" current={choice} onChoose={setChoice} title="MERGE AS IT IS">
            Finish it now: unfinished tickets canceled, the branch merged into its base branch locally (nothing pushed). Then delete.
          </ChoiceButton>
          <ChoiceButton value="discard" current={choice} onChoose={setChoice} title="DELETE ANYWAY">
            Don&apos;t merge. Uncommitted work in the teams&apos; worktrees is lost; the branches stay, you can merge them yourself.
          </ChoiceButton>
        </div>
        {error && <p className={ui.error}>{error}</p>}
        <div className={styles.actions}>
          <button type="button" className={ui.btn} onClick={onClose}>
            Cancel
          </button>
          <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void remove(choice)}>
            {busy && choice === "merge" ? "Merging…" : choice === "merge" ? "Merge and delete" : "Delete anyway"}
          </button>
        </div>
      </Modal>
    );
  }

  return (
    <Modal label={`Delete ${project.name}`} tone="danger" onClose={onClose}>
      <h3 className={ui.title}>Delete {project.name} from Nobody Inc?</h3>
      <div className={styles.kept}>
        <div className={styles.gone}>
          <h4>REMOVED</h4>
          <ul>
            <li>
              <code>.nobody/</code>: teams, agents, schedules
            </li>
            <li>history: mail, tickets, spend</li>
            <li>the teams&apos; worktrees</li>
          </ul>
        </div>
        <div className={styles.stay}>
          <h4>KEPT</h4>
          <ul>
            <li>your code, untouched</li>
            <li>
              every git branch, <code>nobody/*</code> too
            </li>
            <li>your templates and settings</li>
          </ul>
        </div>
      </div>
      <p className={ui.sub}>
        If <code>.nobody/</code> was committed, its deletion shows in git: commit it or not, your choice.
      </p>
      <label className={ui.label}>
        TYPE THE PROJECT NAME TO CONFIRM
        <input className={ui.input} value={typed} placeholder={project.name} autoFocus onChange={(e) => setTyped(e.target.value)} />
      </label>
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.actions}>
        <button type="button" className={ui.btn} onClick={onClose}>
          Cancel
        </button>
        <button type="button" className={ui.btnDanger} disabled={busy || typed !== project.name} onClick={() => void remove()}>
          {busy ? "Deleting…" : "Delete"}
        </button>
      </div>
    </Modal>
  );
}

function ChoiceButton({
  value,
  current,
  onChoose,
  title,
  children,
}: {
  value: Choice;
  current: Choice;
  onChoose: (c: Choice) => void;
  title: string;
  children: ReactNode;
}) {
  return (
    <button type="button" role="radio" aria-checked={current === value} className={styles.option} onClick={() => onChoose(value)}>
      <span className={styles.radio} />
      <span>
        <b>{title}</b>
        <span>{children}</span>
      </span>
    </button>
  );
}
