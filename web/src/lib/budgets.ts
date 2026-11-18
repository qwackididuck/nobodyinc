// Which budgets cover what, as the daemon sees it (budget.Covered, ADR-0013):
// an agent is covered by its ticket in progress and that ticket's feature,
// its own cap, its team's and the staff's; the director by none.

import { type Agent, type BudgetGauge, type BudgetScope, scopeLabel, type Ticket, usd } from "./api";

// gaugeOf finds the gauge of one budget, if it is set.
export function gaugeOf(gauges: BudgetGauge[], scope: BudgetScope): BudgetGauge | undefined {
  return gauges.find((g) => g.scope.kind === scope.kind && (g.scope.id ?? "") === (scope.id ?? ""));
}

// covering lists the gauges of the budgets that cover an agent.
export function covering(agent: Agent, gauges: BudgetGauge[], tickets: Ticket[]): BudgetGauge[] {
  if (agent.team === "") return []; // the director is never held

  const ticket = tickets.find((t) => t.status === "in_progress" && t.assignee === agent.address);
  const scopes: BudgetScope[] = [
    { kind: "agent", id: agent.address },
    { kind: "team", id: agent.team },
    { kind: "staff" },
    ...(ticket ? [{ kind: "ticket" as const, id: ticket.id }] : []),
    ...(ticket?.feature ? [{ kind: "feature" as const, id: ticket.feature }] : []),
  ];

  return scopes.map((s) => gaugeOf(gauges, s)).filter((g): g is BudgetGauge => g !== undefined);
}

// progress is how far a budget is towards its threshold: 1 means reached.
export function progress(g: BudgetGauge): number {
  return g.limit_usd > 0 && g.threshold > 0 ? g.spent_usd / ((g.limit_usd * g.threshold) / 100) : 0;
}

// tightest is the gauge closest to its threshold, if any.
export function tightest(gauges: BudgetGauge[]): BudgetGauge | undefined {
  return [...gauges].sort((a, b) => progress(b) - progress(a))[0];
}

// gaugeText describes a gauge in one line: "team dev: $3.20 / $10.00 (stops at 80%)".
export function gaugeText(g: BudgetGauge): string {
  return `${scopeLabel(g.scope)}: ${usd(g.spent_usd)} / ${usd(g.limit_usd)} (stops at ${g.threshold}%)`;
}
