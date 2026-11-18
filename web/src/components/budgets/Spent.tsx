"use client";

import { usd } from "@/lib/api";
import { useLive } from "@/lib/live";
import styles from "./budgets.module.css";

interface Props {
  spent: number;
  // The budget, if any; without one, only what was spent shows.
  budget?: number;
  // Its own threshold; the project's otherwise.
  threshold?: number;
}

// Spent shows "spent / budget" for a ticket or a feature (ADR-0013): amber
// near the threshold, red at it. Spending shows even while budgets are off.
export function Spent({ spent, budget, threshold }: Props) {
  const { budgets } = useLive();
  const on = budgets?.enabled ?? false;

  if (!on || budget === undefined) {
    return spent > 0 ? <span className={styles.spent}>{usd(spent)} spent</span> : null;
  }

  const stop = threshold ?? budgets?.threshold ?? 80;
  const p = budget > 0 ? (spent / budget) * 100 : 0;
  const tone = p >= stop ? "held" : p >= stop * 0.75 ? "near" : "ok";

  return (
    <span className={styles.spent} data-tone={tone} title={`Its agents stop at ${stop}% of the budget`}>
      {usd(spent)} / {usd(budget)}
    </span>
  );
}
