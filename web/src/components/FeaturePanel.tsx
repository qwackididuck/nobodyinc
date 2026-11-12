"use client";

import { type FormEvent, useState } from "react";

import { listFeatures, startFeature } from "@/lib/api";
import { usePolling } from "@/lib/usePolling";
import styles from "./ControlRoom.module.css";

const REFRESH_MS = 5000;

// FeaturePanel shows the active feature and its git branch, and lets the
// human start a new one. Starting a feature restarts every agent in its
// team's worktree (see docs/workspaces.md).
export function FeaturePanel() {
  const features = usePolling(listFeatures, REFRESH_MS);
  const [open, setOpen] = useState(false);
  const active = (features.data ?? []).find((f) => f.active);

  return (
    <section className={styles.featurePanel}>
      <h2 className={styles.panelTitle}>Feature</h2>
      {active ? (
        <div>
          <p className={styles.agentName}>
            {active.id} — {active.title}
          </p>
          <p className={styles.muted}>
            <code>{active.branch}</code> from <code>{active.base_branch}</code>
          </p>
        </div>
      ) : (
        <p className={styles.empty}>No active feature: agents work in the project directory.</p>
      )}
      {open ? (
        <NewFeatureForm
          onDone={() => {
            setOpen(false);
            void features.refresh();
          }}
        />
      ) : (
        <button type="button" className={styles.smallButton} onClick={() => setOpen(true)}>
          Start a feature
        </button>
      )}
    </section>
  );
}

function NewFeatureForm({ onDone }: { onDone: () => void }) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [base, setBase] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    try {
      await startFeature({ title, description, base_branch: base });
      setError(null);
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={styles.composer} onSubmit={submit}>
      <input placeholder="Title, e.g. Add the order API" value={title} onChange={(e) => setTitle(e.target.value)} />
      <textarea placeholder="What is needed" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
      <input placeholder="Base branch (default: current)" value={base} onChange={(e) => setBase(e.target.value)} />
      <p className={styles.muted}>Agents restart in the worktrees of the new feature.</p>
      <div className={styles.row}>
        <button type="submit" className={styles.primaryButton} disabled={!title || busy}>
          Start
        </button>
        <button type="button" className={styles.linkButton} onClick={onDone}>
          cancel
        </button>
      </div>
      {error && <p className={styles.error}>{error}</p>}
    </form>
  );
}
