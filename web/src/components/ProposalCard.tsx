"use client";

import { useState } from "react";

import { type Feature, refuseProposedFeature, startProposedFeature } from "@/lib/api";
import styles from "./ControlRoom.module.css";

interface Props {
  feature: Feature;
  onDecided: () => void;
}

// ProposalCard shows a feature the director proposed, with its need and
// acceptance criteria, and lets the human start or refuse it. Starting it
// restarts the team agents in their worktrees (not the director); the
// director gets "agree" or "refuse" (with the reason) as an answer.
export function ProposalCard({ feature, onDecided }: Props) {
  const [refusing, setRefusing] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function decide(action: () => Promise<unknown>) {
    setBusy(true);
    try {
      await action();
      setError(null);
      onDecided();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <article className={styles.proposal}>
      <p className={styles.agentName}>
        {feature.id} — {feature.title} <span className={styles.muted}>proposed by {feature.created_by}</span>
      </p>
      {feature.description && (
        <p className={styles.body}>
          <strong>Need:</strong> {feature.description}
        </p>
      )}
      {feature.acceptance_criteria && (
        <p className={styles.body}>
          <strong>Acceptance criteria:</strong> {feature.acceptance_criteria}
        </p>
      )}
      {refusing ? (
        <div className={styles.row}>
          <input className={styles.grow} placeholder="Reason (passed on to the director)" value={reason} onChange={(e) => setReason(e.target.value)} />
          <button type="button" className={styles.denyButton} disabled={busy} onClick={() => void decide(() => refuseProposedFeature(feature.id, reason))}>
            Refuse
          </button>
          <button type="button" className={styles.linkButton} onClick={() => setRefusing(false)}>
            cancel
          </button>
        </div>
      ) : (
        <div className={styles.row}>
          <button type="button" className={styles.primaryButton} disabled={busy} onClick={() => void decide(() => startProposedFeature(feature.id))}>
            Start
          </button>
          <button type="button" className={styles.smallButton} disabled={busy} onClick={() => setRefusing(true)}>
            Refuse…
          </button>
          <span className={styles.muted}>Starting moves the teams into the feature&apos;s worktrees.</span>
        </div>
      )}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}
