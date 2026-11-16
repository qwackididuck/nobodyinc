"use client";

import { type FormEvent, useState } from "react";

import { startFeature } from "@/lib/api";
import { useLive } from "@/lib/live";
import styles from "./office.module.css";

interface Props {
  onOpenDirector: () => void;
}

// Banner sits above the office: the active feature, its branch and how many
// of its tickets are done, and the director's proposal waiting for the human.
// Without an active feature, the human can start one directly (docs/workspaces.md).
export function Banner({ onOpenDirector }: Props) {
  const { features, tickets } = useLive();
  const active = features.find((f) => f.active);
  const proposals = features.filter((f) => f.status === "proposed");

  const own = active ? tickets.filter((t) => t.feature === active.id && t.status !== "canceled") : [];
  const done = own.filter((t) => t.status === "done").length;

  return (
    <div className={styles.banner}>
      {active ? (
        <div className={styles.bannerFeature}>
          <span className={styles.kicker}>Active feature</span>
          <strong className={styles.bannerTitle}>{active.title}</strong>
          <span className={styles.bannerMeta}>
            <code>{active.branch}</code> · {done} of {own.length} tickets done
          </span>
          <span className={styles.progress} aria-hidden="true">
            <span style={{ width: own.length ? `${(done / own.length) * 100}%` : 0 }} />
          </span>
        </div>
      ) : (
        <StartFeature />
      )}
      {proposals.length > 0 && (
        <button type="button" className={styles.bannerProposal} onClick={onOpenDirector}>
          <span className={styles.kicker}>{proposals.length === 1 ? "Proposal from the director" : `${proposals.length} proposals`}</span>
          <span>{proposals[0].title}</span>
        </button>
      )}
    </div>
  );
}

// StartFeature starts a feature as the human: its branch is created from the
// base branch, and the teams restart in their worktrees.
function StartFeature() {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [base, setBase] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await startFeature({ title: title.trim(), description, base_branch: base.trim() });
      setOpen(false);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <div className={styles.bannerFeature}>
        <span className={styles.kicker}>No active feature</span>
        <button type="button" className={styles.bannerLink} onClick={() => setOpen(true)}>
          Start a feature
        </button>
      </div>
    );
  }

  return (
    <form className={styles.startFeature} onSubmit={submit}>
      <input id="feature-title" className={styles.bannerInput} placeholder="Title, e.g. Add the order API" aria-label="Feature title" value={title} onChange={(e) => setTitle(e.target.value)} required />
      <input id="feature-need" className={styles.bannerInput} placeholder="What is needed" aria-label="What is needed" value={description} onChange={(e) => setDescription(e.target.value)} />
      <input id="feature-base" className={styles.bannerInput} placeholder="Base branch (default: current)" aria-label="Base branch" value={base} onChange={(e) => setBase(e.target.value)} />
      <button type="submit" className={styles.bannerButton} disabled={busy || !title.trim()}>
        Start
      </button>
      <button type="button" className={styles.bannerLink} onClick={() => setOpen(false)}>
        Cancel
      </button>
      {error && <span className={styles.bannerError}>{error}</span>}
    </form>
  );
}
