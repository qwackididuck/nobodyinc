"use client";

// Budget fields shared by the forms (ADR-0013): whether budgets are on, how
// to read an amount, and the editor of an agent's, a team's or the staff's
// cap, which the daemon writes to .nobody/budgets.yaml.

import { useState } from "react";

import { type BudgetCap, type BudgetScope, type BudgetSettings, updateBudgets, usd } from "@/lib/api";
import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import styles from "./budgets.module.css";

// useBudgetsOn tells whether budgets are on for the project: every budget
// field hides while they are off (or not loaded yet).
export function useBudgetsOn(): boolean {
  return useLive().budgets?.enabled ?? false;
}

// parseUSD reads an amount typed in a field: undefined when empty, NaN when
// it is not a positive number.
export function parseUSD(text: string): number | undefined {
  const t = text.trim().replace(/^\$/, "");
  if (t === "") return undefined;
  const n = Number(t);
  return Number.isFinite(n) && n > 0 ? n : Number.NaN;
}

// capOf reads one cap of the settings.
export function capOf(s: BudgetSettings, scope: BudgetScope): BudgetCap | undefined {
  if (scope.kind === "staff") return s.staff;
  if (scope.kind === "team") return s.teams?.[scope.id ?? ""];
  if (scope.kind === "agent") return s.agents?.[scope.id ?? ""];
  return undefined;
}

// withCap returns the settings with one cap set, or removed with undefined.
export function withCap(s: BudgetSettings, scope: BudgetScope, cap: BudgetCap | undefined): BudgetSettings {
  const id = scope.id ?? "";
  const set = <T,>(m: Record<string, T> | undefined, v: T | undefined) => {
    const next = { ...m };
    if (v === undefined) delete next[id];
    else next[id] = v;
    return next;
  };

  if (scope.kind === "staff") return { ...s, staff: cap };
  if (scope.kind === "team") return { ...s, teams: set(s.teams, cap) };
  return { ...s, agents: set(s.agents, cap) };
}

interface Props {
  scope: BudgetScope;
  // What the cap is about, e.g. "Cap on dev1@dev".
  title: string;
}

// CapEditor shows and changes one cap: a limit in USD, monthly or
// cumulative, and an optional threshold. Nothing while budgets are off.
export function CapEditor({ scope, title }: Props) {
  const { budgets, reload } = useLive();
  const current = budgets ? capOf(budgets, scope) : undefined;
  const [limit, setLimit] = useState(current ? String(current.limit_usd) : "");
  const [monthly, setMonthly] = useState(current?.monthly ?? true);
  const [threshold, setThreshold] = useState(current?.threshold ? String(current.threshold) : "");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (!budgets?.enabled) return null;

  async function save(cap: BudgetCap | undefined) {
    if (!budgets) return;
    setBusy(true);
    setError(null);
    try {
      await updateBudgets(withCap(budgets, scope, cap));
      reload("budgets");
      setNote(cap ? `Saved: ${usd(cap.limit_usd)}${cap.monthly ? " a month" : " in all"}.` : "Cap removed.");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const amount = parseUSD(limit);
  const percent = threshold.trim() === "" ? undefined : Number(threshold);
  const valid = amount !== undefined && !Number.isNaN(amount) && (percent === undefined || (percent >= 1 && percent <= 100));

  return (
    <fieldset className={styles.cap}>
      <legend className={ui.label}>{title.toUpperCase()}</legend>
      <p className={ui.sub}>
        A guard in USD at list price: at the threshold ({budgets.threshold}% unless set here), the agents it covers stop at the end of
        their turn and the director asks you for an extension.
      </p>
      <div className={styles.capRow}>
        <label className={ui.label}>
          LIMIT (USD)
          <input className={ui.input} inputMode="decimal" placeholder="none" value={limit} onChange={(e) => setLimit(e.target.value)} />
        </label>
        <label className={ui.label}>
          THRESHOLD (%)
          <input className={ui.input} inputMode="numeric" placeholder={String(budgets.threshold)} value={threshold} onChange={(e) => setThreshold(e.target.value)} />
        </label>
        <label className={styles.check}>
          <input type="checkbox" checked={monthly} onChange={(e) => setMonthly(e.target.checked)} /> Reset every month
        </label>
      </div>
      <div className={styles.capRow}>
        <button
          type="button"
          className={ui.btn}
          disabled={busy || !valid}
          onClick={() => amount !== undefined && void save({ limit_usd: amount, monthly, ...(percent ? { threshold: percent } : {}) })}
        >
          Save cap
        </button>
        {current && (
          <button type="button" className={ui.link} disabled={busy} onClick={() => void save(undefined)}>
            Remove the cap
          </button>
        )}
        {note && <span className={ui.sub}>{note}</span>}
      </div>
      {error && <p className={ui.error}>{error}</p>}
    </fieldset>
  );
}
