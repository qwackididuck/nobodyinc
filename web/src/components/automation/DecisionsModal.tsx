"use client";

import { useState } from "react";

import { type Decision, markDecisionsSeen } from "@/lib/api";
import { useLive } from "@/lib/live";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./automation.module.css";

// unseenDecisions are the decisions the human has not acknowledged yet.
export function unseenDecisions(decisions: Decision[]): Decision[] {
  return decisions.filter((d) => !d.seen);
}

// The heaviest first, then the newest: what to read before the rest.
const WEIGHT = { high: 0, medium: 1, low: 2 } as const;

function byImportance(a: Decision, b: Decision): number {
  return WEIGHT[a.impact] - WEIGHT[b.impact] || b.id - a.id;
}

// DecisionsModal is the report of what the director decided in the human's
// place (Nobody mode): the unseen decisions first, high impact on top and
// highlighted, then the ones already acknowledged.
export function DecisionsModal({ onClose }: { onClose: () => void }) {
  const { decisions } = useLive();
  const [error, setError] = useState<string | null>(null);
  const unseen = unseenDecisions(decisions).sort(byImportance);
  const seen = decisions.filter((d) => d.seen);

  async function acknowledge(ids: number[]) {
    setError(null);
    try {
      await markDecisionsSeen(ids);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <Modal label="Decisions taken in your absence" size="wide" onClose={onClose}>
      <h3 className={ui.title}>Decisions taken in your absence</h3>
      <p className={ui.sub}>
        The director answered these questions in your place. Check them, and tell it if one has to change.
      </p>
      {decisions.length === 0 && <p className={ui.sub}>Nothing was decided for you.</p>}
      {unseen.length > 0 && (
        <>
          <div className={styles.listHead}>
            <h4>TO REVIEW ({unseen.length})</h4>
            <button type="button" className={ui.btn} onClick={() => void acknowledge([])}>
              Mark all as seen
            </button>
          </div>
          <ul className={styles.decisions}>
            {unseen.map((d) => (
              <DecisionCard key={d.id} d={d} onSeen={() => void acknowledge([d.id])} />
            ))}
          </ul>
        </>
      )}
      {seen.length > 0 && (
        <>
          <div className={styles.listHead}>
            <h4>SEEN ({seen.length})</h4>
          </div>
          <ul className={styles.decisions}>
            {seen.map((d) => (
              <DecisionCard key={d.id} d={d} />
            ))}
          </ul>
        </>
      )}
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.actions}>
        <button type="button" className={ui.btn} onClick={onClose}>
          Close
        </button>
      </div>
    </Modal>
  );
}

function DecisionCard({ d, onSeen }: { d: Decision; onSeen?: () => void }) {
  return (
    <li className={styles.decision} data-impact={d.impact} data-seen={d.seen}>
      <div className={styles.decisionHead}>
        <span className={styles.impact} data-impact={d.impact}>
          {d.impact.toUpperCase()} IMPACT
        </span>
        <span className={styles.asker}>asked by {d.asked_by}</span>
        <time className={styles.when} dateTime={d.decided_at}>
          {new Date(d.decided_at).toLocaleString()}
        </time>
      </div>
      <div className={styles.question}>{d.question}</div>
      <p className={styles.answer}>
        <b>Decided:</b> {d.decision}
      </p>
      <p className={styles.why}>
        <b>Why:</b> {d.rationale}
      </p>
      {onSeen && (
        <button type="button" className={ui.btn} onClick={onSeen}>
          Seen
        </button>
      )}
    </li>
  );
}
