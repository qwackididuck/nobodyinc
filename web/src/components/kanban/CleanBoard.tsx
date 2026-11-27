"use client";

import { useState } from "react";

import { cleanBoard, type Ticket } from "@/lib/api";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./kanban.module.css";

interface Props {
  tickets: Ticket[];
  onCleaned: (ids: string[]) => void;
}

type Scope = "closed" | "all";

// CleanBoard is the Kanban's "Clean board" button: a dialog offers to delete
// the done and canceled tickets, or the whole board, for good (the shredder,
// in bulk). The agents working on a deleted ticket are told to stop. Tickets
// of finished features are archived and stay.
export function CleanBoard({ tickets, onCleaned }: Props) {
  const [opened, setOpened] = useState(false);
  const [choice, setChoice] = useState<Scope>("closed");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const closed = tickets.filter((t) => t.status === "done" || t.status === "canceled");
  const working = tickets.filter((t) => t.status === "in_progress" && t.assignee);
  const count = choice === "closed" ? closed.length : tickets.length;

  async function clean() {
    setBusy(true);
    try {
      onCleaned(await cleanBoard(choice));
      setOpened(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function openDialog() {
    setChoice(closed.length > 0 ? "closed" : "all");
    setError(null);
    setOpened(true);
  }

  return (
    <>
      <button type="button" className={ui.btn} disabled={tickets.length === 0} onClick={openDialog}>
        Clean board
      </button>
      {opened && (
        <Modal label="Clean the board" tone="danger" onClose={() => setOpened(false)}>
          <h3 className={ui.title}>Clean the board?</h3>
          <label className={styles.modalCheck}>
            <input type="radio" name="clean-scope" checked={choice === "closed"} disabled={closed.length === 0} onChange={() => setChoice("closed")} />
            <span>
              Done and canceled tickets only ({closed.length}).
              <br />
              <em>Open and in-progress tickets stay.</em>
            </span>
          </label>
          <label className={styles.modalCheck}>
            <input type="radio" name="clean-scope" checked={choice === "all"} onChange={() => setChoice("all")} />
            <span>
              The whole board ({tickets.length}).
              <br />
              <em>Every ticket goes, open and in progress too.</em>
            </span>
          </label>
          <p className={ui.sub}>The tickets, their comments and their deliverables are deleted for good. Messages about them stay. Archived tickets stay.</p>
          {choice === "all" && working.length > 0 && (
            <p className={styles.modalWarning}>
              {[...new Set(working.map((t) => t.assignee?.split("@")[0]))].join(", ")} {working.length > 1 ? "are" : "is"} working on a ticket: their job is
              stopped (they get an urgent notice). Changes they did not commit stay in their worktree.
            </p>
          )}
          {error && <p className={ui.error}>{error}</p>}
          <div className={styles.actions}>
            <button type="button" className={ui.btnDanger} disabled={busy || count === 0} onClick={() => void clean()}>
              {`Delete ${count} ticket${count === 1 ? "" : "s"}`}
            </button>
            <button type="button" className={ui.link} onClick={() => setOpened(false)}>
              Cancel
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
