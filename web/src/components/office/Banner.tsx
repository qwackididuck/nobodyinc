"use client";

import { useLive } from "@/lib/live";
import styles from "./office.module.css";

interface Props {
  onOpenDirector: () => void;
}

// Banner sits above the office: the active feature, its branch and how many
// of its tickets are done, and the director's proposal waiting for the human.
export function Banner({ onOpenDirector }: Props) {
  const { features, tickets } = useLive();
  const active = features.find((f) => f.active);
  const proposals = features.filter((f) => f.status === "proposed");

  if (!active && proposals.length === 0) return null;

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
        <div className={styles.bannerFeature}>
          <span className={styles.kicker}>No active feature</span>
        </div>
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
