"use client";

import { useEffect, useState } from "react";

import { type BudgetGauge, getSpend, scopeLabel, type SpendLine, type SpendSummary, usd } from "@/lib/api";
import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import styles from "./review.module.css";
import spend from "./spend.module.css";

interface Props {
  onClose: () => void;
}

// How many months the selector offers, the current one included.
const MONTHS = 12;

// SpendPanel is what the spending board tells (ADR-0013): what the agents
// spent in a month, by team, agent, feature and schedule, and where every
// budget stands now. Amounts are Claude Code's estimate at list price: a
// guard, not an invoice.
export function SpendPanel({ onClose }: Props) {
  const live = useLive();
  const current = live.spend?.month ?? monthOf(new Date());
  const [month, setMonth] = useState(current);
  const [past, setPast] = useState<SpendSummary | null>(null);
  const [error, setError] = useState<string | null>(null);

  // The current month is live; a past one is read once.
  useEffect(() => {
    if (month === current) return;

    let stale = false;
    getSpend(month)
      .then((s) => !stale && setPast(s))
      .catch((e: unknown) => !stale && setError(e instanceof Error ? e.message : String(e)));

    return () => {
      stale = true;
    };
  }, [month, current]);

  const summary = month === current ? live.spend : past?.month === month ? past : null;

  return (
    <aside className={styles.panel} role="dialog" aria-label="Spending and budgets">
      <div className={styles.head}>
        <div>
          <span className={styles.kicker}>The spending board</span>
          <h3 className={ui.title}>{summary ? `${usd(summary.total_usd)} spent` : "Spending"}</h3>
          <p className={ui.sub}>Claude Code&apos;s estimate at list price: a guard against runaway agents, not an invoice.</p>
        </div>
        <button type="button" className={ui.link} onClick={onClose}>
          Close
        </button>
      </div>
      <div className={styles.body}>
        <label className={spend.month}>
          Month{" "}
          <select
            value={month}
            onChange={(e) => {
              setError(null);
              setMonth(e.target.value);
            }}
          >
            {recentMonths(current).map((m) => (
              <option key={m} value={m}>
                {monthLabel(m)}
              </option>
            ))}
          </select>
        </label>
        {error && <p className={ui.error}>{error}</p>}
        {!summary && !error && <p className={ui.sub}>Loading…</p>}
        {summary && (
          <>
            <Gauges gauges={month === current ? summary.gauges : []} enabled={live.budgets?.enabled ?? true} past={month !== current} />
            <Breakdown title="Teams" lines={summary.teams} total={summary.total_usd} />
            <Breakdown title="Agents" lines={summary.agents} total={summary.total_usd} />
            <Breakdown title="Features" lines={summary.features} total={summary.total_usd} />
            <Breakdown title="Scheduled tasks" lines={summary.schedules} total={summary.total_usd} />
          </>
        )}
      </div>
    </aside>
  );
}

function Gauges({ gauges, enabled, past }: { gauges: BudgetGauge[]; enabled: boolean; past: boolean }) {
  return (
    <div className={styles.section}>
      <h4 className={spend.heading}>Budgets</h4>
      {!enabled && <p className={ui.sub}>Budgets are turned off for this project (Settings); spending is still measured.</p>}
      {enabled && past && <p className={ui.sub}>Budgets show where they stand now: pick the current month.</p>}
      {enabled && !past && gauges.length === 0 && <p className={ui.sub}>No budget is set.</p>}
      <ul className={spend.list}>
        {gauges.map((g) => (
          <li key={`${g.scope.kind}:${g.scope.id ?? ""}`}>
            <span className={spend.name}>
              {scopeLabel(g.scope)}
              {g.monthly && <em> · this month</em>}
              {g.held && <strong className={spend.held}> · holding its agents</strong>}
            </span>
            <span className={spend.amount}>
              {usd(g.spent_usd)} / {usd(g.limit_usd)}
            </span>
            <GaugeBar spent={g.spent_usd} limit={g.limit_usd} threshold={g.threshold} />
          </li>
        ))}
      </ul>
    </div>
  );
}

// GaugeBar fills up to what was spent, with a mark at the threshold.
export function GaugeBar({ spent, limit, threshold }: { spent: number; limit: number; threshold: number }) {
  const ratio = limit > 0 ? spent / limit : 0;
  const tone = ratio * 100 >= threshold ? "held" : ratio * 100 >= threshold * 0.75 ? "near" : "ok";

  return (
    <span className={spend.bar} data-tone={tone} title={`${Math.round(ratio * 100)}% spent; agents stop at ${threshold}%`}>
      <span className={spend.fill} style={{ width: `${Math.min(ratio, 1) * 100}%` }} />
      <span className={spend.threshold} style={{ left: `${threshold}%` }} />
    </span>
  );
}

function Breakdown({ title, lines, total }: { title: string; lines: SpendLine[]; total: number }) {
  if (lines.length === 0) return null;

  return (
    <div className={styles.section}>
      <h4 className={spend.heading}>{title}</h4>
      <ul className={spend.list}>
        {lines.map((l) => (
          <li key={l.key}>
            <span className={spend.name}>{l.label ? `${l.key} · ${l.label}` : l.key}</span>
            <span className={spend.amount}>{usd(l.spent_usd)}</span>
            <span className={spend.bar} data-tone="share">
              <span className={spend.fill} style={{ width: `${total > 0 ? (l.spent_usd / total) * 100 : 0}%` }} />
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// monthOf is a date's month, "YYYY-MM", in the browser's time zone (the
// daemon counts in its machine's, usually the same).
function monthOf(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

// recentMonths lists the last MONTHS months, newest first.
function recentMonths(current: string): string[] {
  const [year, month] = current.split("-").map(Number);
  return Array.from({ length: MONTHS }, (_, i) => monthOf(new Date(year, month - 1 - i, 1)));
}

function monthLabel(m: string): string {
  const [year, month] = m.split("-").map(Number);
  return new Date(year, month - 1, 1).toLocaleDateString([], { month: "long", year: "numeric" });
}
