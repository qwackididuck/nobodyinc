"use client";

import { type FormEvent, useState } from "react";

import {
  acceptBudgetProposal,
  acceptScheduleProposal,
  type Agent,
  type Approval,
  type BudgetOutcome,
  type BudgetProposal,
  type BudgetRequest,
  decideApproval,
  type PermissionUpdate,
  extendBudget,
  type Feature,
  type Message,
  refuseBudget,
  refuseBudgetProposal,
  refuseProposedFeature,
  refuseScheduleProposal,
  type ScheduleProposal,
  scopeLabel,
  sendMessage,
  startProposedFeature,
  usd,
} from "@/lib/api";
import { useLive } from "@/lib/live";
import { parseUSD, useBudgetsOn } from "../budgets/CapEditor";
import { useOpenSchedules } from "../schedules/nav";
import { recurrenceText } from "../schedules/scheduling";
import styles from "./chat.module.css";

// Cards are what waits for the human in a chat. Each one handles its own
// action and error.

function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(action: () => Promise<unknown>, after?: () => void) {
    setBusy(true);
    try {
      await action();
      setError(null);
      after?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return { busy, error, run };
}

const MAX_INPUT_CHARS = 600;

// describeTool shows what the agent wants to do: the command for Bash, the
// file for edits, the raw arguments otherwise.
function describeTool(a: Approval): string {
  const input = a.tool_input ?? {};
  const command = input["command"];
  const file = input["file_path"];
  let text: string;
  if (typeof command === "string") text = command;
  else if (typeof file === "string") text = file;
  else text = JSON.stringify(input, null, 2);
  return text.length > MAX_INPUT_CHARS ? `${text.slice(0, MAX_INPUT_CHARS)}…` : text;
}

// ApprovalCard is a permission request: the agent is blocked until the human
// allows or denies it (docs/approvals.md).
export function ApprovalCard({ approval, onDecided }: { approval: Approval; onDecided: () => void }) {
  const { busy, error, run } = useAction();
  const [reason, setReason] = useState("");

  return (
    <article className={styles.card} data-kind="approval">
      <span className={styles.kicker}>Permission request</span>
      <p className={styles.cardText}>{approval.agent} wants to use {approval.tool_name}</p>
      <pre className={styles.code}>{describeTool(approval)}</pre>
      <input
        className={styles.input}
        placeholder="Reason, shown to the agent if you deny"
        aria-label="Reason for denying"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
      <div className={styles.actions}>
        <button type="button" className={styles.btnPrimary} disabled={busy} onClick={() => void run(() => decideApproval(approval.id, true), onDecided)}>
          Allow
        </button>
        <button type="button" className={styles.btn} disabled={busy} onClick={() => void run(() => decideApproval(approval.id, false, reason), onDecided)}>
          Deny
        </button>
      </div>
      {(approval.suggestions ?? []).length > 0 && (
        <div className={styles.actions}>
          {(approval.suggestions ?? []).map((s, i) => (
            <button
              key={i}
              type="button"
              className={styles.btn}
              disabled={busy}
              onClick={() => void run(() => decideApproval(approval.id, true, "", i), onDecided)}
            >
              {describeSuggestion(s)}
            </button>
          ))}
        </div>
      )}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

// UrgentQuestion is an urgent message the human has not answered yet. The
// answer goes back as a reply to it.
export function UrgentQuestion({ message, agent }: { message: Message; agent: Agent }) {
  const { busy, error, run } = useAction();
  const [answer, setAnswer] = useState("");

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!answer.trim()) return;
    void run(() =>
      sendMessage({
        to: agent.address,
        act: "inform",
        priority: "urgent",
        subject: `Re: ${message.subject}`,
        body: answer.trim(),
        in_reply_to: message.id,
      }),
    );
  }

  return (
    <form className={styles.card} data-kind="urgent" onSubmit={submit}>
      <span className={styles.kicker}>Urgent question</span>
      {message.subject && <strong className={styles.cardTitle}>{message.subject}</strong>}
      {message.body && <p className={styles.cardText}>{message.body}</p>}
      <input
        className={styles.input}
        placeholder={`Answer ${agent.name}…`}
        aria-label="Your answer"
        value={answer}
        onChange={(e) => setAnswer(e.target.value)}
      />
      <div className={styles.actions}>
        <button type="submit" className={styles.btnPrimary} disabled={busy || !answer.trim()}>
          Answer
        </button>
      </div>
      {error && <p className={styles.error}>{error}</p>}
    </form>
  );
}

// FeatureProposal is a feature the director proposed. Starting it moves the
// teams into the feature's worktrees; the director gets "agree", or "refuse"
// with the reason (docs/god.md). Once decided, it shows the decision.
export function FeatureProposal({ feature, onDecided }: { feature: Feature; onDecided: () => void }) {
  const { busy, error, run } = useAction();
  const [refusing, setRefusing] = useState(false);
  const [reason, setReason] = useState("");
  // The budget the director proposes, which the human may change before
  // starting (ADR-0013); empty starts it without one.
  const budgetsOn = useBudgetsOn();
  const [budget, setBudget] = useState(feature.budget_usd !== undefined ? String(feature.budget_usd) : "");
  const amount = parseUSD(budget);
  const start = () =>
    budgetsOn && (amount ?? null) !== (feature.budget_usd ?? null)
      ? startProposedFeature(feature.id, { budget_usd: amount ?? null })
      : startProposedFeature(feature.id);

  return (
    <article className={styles.card} data-kind="proposal">
      <span className={styles.kicker}>Feature proposal · {feature.id}</span>
      <strong className={styles.cardTitle}>{feature.title}</strong>
      {feature.description && <p className={styles.cardText}>{feature.description}</p>}
      {feature.acceptance_criteria && (
        <>
          <span className={styles.kicker}>Acceptance criteria</span>
          <p className={styles.cardText}>{feature.acceptance_criteria}</p>
        </>
      )}
      {feature.status === "proposed" && budgetsOn && (
        <label className={styles.kicker}>
          Budget (USD){feature.budget_usd !== undefined ? `, proposed: ${usd(feature.budget_usd)}` : ", optional"}
          <input
            className={styles.input}
            inputMode="decimal"
            placeholder="none"
            aria-label="Feature budget in USD"
            value={budget}
            onChange={(e) => setBudget(e.target.value)}
          />
        </label>
      )}
      {feature.status !== "proposed" && feature.budget_usd !== undefined && <p className={styles.cardText}>Budget: {usd(feature.budget_usd)}</p>}
      {feature.status === "started" && <p className={styles.done}>Started{feature.active ? " · active" : ""}</p>}
      {feature.status === "refused" && <p className={styles.refused}>Refused{feature.refusal_reason ? `: ${feature.refusal_reason}` : ""}</p>}
      {feature.status === "proposed" &&
        (refusing ? (
          <div className={styles.actions}>
            <input
              className={styles.input}
              placeholder="Reason, passed on to the director"
              aria-label="Reason for refusing"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <button type="button" className={styles.btn} disabled={busy} onClick={() => void run(() => refuseProposedFeature(feature.id, reason), onDecided)}>
              Refuse
            </button>
            <button type="button" className={styles.link} onClick={() => setRefusing(false)}>
              Cancel
            </button>
          </div>
        ) : (
          <div className={styles.actions}>
            <button type="button" className={styles.btnPrimary} disabled={busy || Number.isNaN(amount)} onClick={() => void run(start, onDecided)}>
              Start
            </button>
            <button type="button" className={styles.btn} disabled={busy} onClick={() => setRefusing(true)}>
              Refuse…
            </button>
          </div>
        ))}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

// The outcomes offered when refusing an extension (ADR-0013, point 5).
const OUTCOMES: { value: BudgetOutcome; label: string }[] = [
  { value: "hold", label: "Keep on hold" },
  { value: "release", label: "Give the ticket back to the queue" },
  { value: "cancel", label: "Cancel the ticket" },
];

// BudgetRequestCard is an extension request the director passed on (ADR-0013):
// a budget at its threshold holds its agents. Extend grants an amount (the
// director's recommendation, editable); Refuse applies one outcome to every
// held agent, which "per agent" changes one by one, optionally after a short
// turn to commit the work in progress. Once decided, it shows the decision.
export function BudgetRequestCard({ request: r, onDecided }: { request: BudgetRequest; onDecided: () => void }) {
  const { busy, error, run } = useAction();
  const { tickets } = useLive();
  const [amount, setAmount] = useState(String(r.recommended_usd ?? r.amount_usd ?? ""));
  const [refusing, setRefusing] = useState(false);
  const [common, setCommon] = useState<BudgetOutcome>("hold");
  const [perAgent, setPerAgent] = useState<Record<string, BudgetOutcome> | null>(null);
  const [commitFirst, setCommitFirst] = useState(false);
  const [reason, setReason] = useState("");
  const extension = parseUSD(amount);

  // An agent without a ticket can only stay on hold.
  const ticketOf = (agent: string) => tickets.find((t) => t.status === "in_progress" && t.assignee === agent)?.id;
  const outcomeOf = (agent: string): BudgetOutcome => (ticketOf(agent) ? (perAgent?.[agent] ?? common) : "hold");
  const refuse = () =>
    refuseBudget(r.id, {
      outcomes: Object.fromEntries(r.held_agents.map((a) => [a, outcomeOf(a)])),
      commit_first: commitFirst,
      reason,
    });

  return (
    <article className={styles.card} data-kind="budget">
      <span className={styles.kicker}>Budget request · {r.id}</span>
      <strong className={styles.cardTitle}>
        {scopeLabel(r.scope)}: {usd(r.spent_usd)} spent of {usd(r.limit_usd)}
      </strong>
      {r.held_agents.length > 0 && <p className={styles.cardText}>On hold: {r.held_agents.join(", ")}</p>}
      {r.justification && (
        <p className={styles.cardText}>
          {r.requested_by} asks for {usd(r.amount_usd ?? 0)} more: {r.justification}
        </p>
      )}
      {r.director_opinion && (
        <p className={styles.cardText}>
          The director{r.recommended_usd !== undefined ? ` recommends ${usd(r.recommended_usd)}` : ""}: {r.director_opinion}
        </p>
      )}
      {r.feature_remaining_usd !== undefined && (
        <p className={styles.cardText}>Its feature has {usd(Math.max(r.feature_remaining_usd, 0))} left to allocate; an extension beyond it raises the feature&apos;s budget.</p>
      )}
      {r.status === "extended" && <p className={styles.done}>Extended by {usd(r.extended_usd ?? 0)}</p>}
      {r.status === "refused" && <p className={styles.refused}>Refused{r.refusal?.reason ? `: ${r.refusal.reason}` : ""}</p>}
      {r.status === "closed" && <p className={styles.done}>Closed: {r.closed_reason}</p>}
      {r.status === "awaiting_human" &&
        (refusing ? (
          <div className={styles.cardText}>
            <label>
              For every agent{" "}
              <select aria-label="What happens to the held agents" value={common} onChange={(e) => setCommon(e.target.value as BudgetOutcome)}>
                {OUTCOMES.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>{" "}
            {r.held_agents.length > 1 && perAgent === null && (
              <button type="button" className={styles.link} onClick={() => setPerAgent({})}>
                per agent
              </button>
            )}
            {perAgent !== null && (
              <ul>
                {r.held_agents.map((a) => (
                  <li key={a}>
                    {a}
                    {ticketOf(a) ? ` (${ticketOf(a)}) ` : " (no ticket: stays on hold) "}
                    {ticketOf(a) && (
                      <select aria-label={`What happens to ${a}`} value={outcomeOf(a)} onChange={(e) => setPerAgent({ ...perAgent, [a]: e.target.value as BudgetOutcome })}>
                        {OUTCOMES.map((o) => (
                          <option key={o.value} value={o.value}>
                            {o.label}
                          </option>
                        ))}
                      </select>
                    )}
                  </li>
                ))}
              </ul>
            )}
            <label>
              <input type="checkbox" checked={commitFirst} onChange={(e) => setCommitFirst(e.target.checked)} /> Commit the work in progress first (one
              short turn each)
            </label>
            <input
              className={styles.input}
              placeholder="Reason, passed on to the director"
              aria-label="Reason for refusing"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <div className={styles.actions}>
              <button type="button" className={styles.btn} disabled={busy} onClick={() => void run(refuse, onDecided)}>
                Refuse
              </button>
              <button type="button" className={styles.link} onClick={() => setRefusing(false)}>
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <div className={styles.actions}>
            <input
              className={styles.input}
              inputMode="decimal"
              aria-label="Extension in USD"
              placeholder="USD"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
            <button
              type="button"
              className={styles.btnPrimary}
              disabled={busy || extension === undefined || Number.isNaN(extension)}
              onClick={() => extension !== undefined && void run(() => extendBudget(r.id, extension), onDecided)}
            >
              Extend +{extension !== undefined && !Number.isNaN(extension) ? usd(extension) : "$…"}
            </button>
            <button type="button" className={styles.btn} disabled={busy} onClick={() => setRefusing(true)}>
              Refuse…
            </button>
          </div>
        ))}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

// BudgetProposalCard is a cap the director proposed (ADR-0013, point 8):
// Accept writes it, possibly with the limit changed; Refuse sends the reason
// back. Once decided, it shows the decision.
export function BudgetProposalCard({ proposal: p, onDecided }: { proposal: BudgetProposal; onDecided: () => void }) {
  const { busy, error, run } = useAction();
  const [limit, setLimit] = useState(String(p.cap.limit_usd));
  const [refusing, setRefusing] = useState(false);
  const [reason, setReason] = useState("");
  const amount = parseUSD(limit);
  const accept = () =>
    amount !== undefined && amount !== p.cap.limit_usd ? acceptBudgetProposal(p.id, { ...p.cap, limit_usd: amount }) : acceptBudgetProposal(p.id);

  return (
    <article className={styles.card} data-kind="proposal">
      <span className={styles.kicker}>Cap proposal · {p.id}</span>
      <strong className={styles.cardTitle}>
        {usd(p.cap.limit_usd)} on {scopeLabel(p.scope)}, {p.cap.monthly ? "every month" : "in all"}
        {p.cap.threshold ? `, stops at ${p.cap.threshold}%` : ""}
      </strong>
      {p.reason && <p className={styles.cardText}>{p.reason}</p>}
      {p.status === "accepted" && <p className={styles.done}>Accepted: {usd(p.cap.limit_usd)}</p>}
      {p.status === "refused" && <p className={styles.refused}>Refused{p.refusal_reason ? `: ${p.refusal_reason}` : ""}</p>}
      {p.status === "proposed" &&
        (refusing ? (
          <div className={styles.actions}>
            <input
              className={styles.input}
              placeholder="Reason, passed on to the director"
              aria-label="Reason for refusing"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <button type="button" className={styles.btn} disabled={busy} onClick={() => void run(() => refuseBudgetProposal(p.id, reason), onDecided)}>
              Refuse
            </button>
            <button type="button" className={styles.link} onClick={() => setRefusing(false)}>
              Cancel
            </button>
          </div>
        ) : (
          <div className={styles.actions}>
            <input className={styles.input} inputMode="decimal" aria-label="Cap in USD" value={limit} onChange={(e) => setLimit(e.target.value)} />
            <button type="button" className={styles.btnPrimary} disabled={busy || amount === undefined || Number.isNaN(amount)} onClick={() => void run(accept, onDecided)}>
              Accept
            </button>
            <button type="button" className={styles.btn} disabled={busy} onClick={() => setRefusing(true)}>
              Refuse…
            </button>
          </div>
        ))}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

// ScheduleProposalCard is a scheduled task the director proposed (ADR-0012):
// Accept writes it as it is, Edit opens it in the Schedules tab to change it
// before accepting, Refuse sends the reason back. Once decided, it shows the
// decision.
export function ScheduleProposalCard({ proposal: p, onDecided }: { proposal: ScheduleProposal; onDecided: () => void }) {
  const { busy, error, run } = useAction();
  const openSchedules = useOpenSchedules();
  const [refusing, setRefusing] = useState(false);
  const [reason, setReason] = useState("");
  const s = p.schedule;

  return (
    <article className={styles.card} data-kind="proposal">
      <span className={styles.kicker}>Schedule proposal · {p.id}</span>
      <strong className={styles.cardTitle}>{s.title}</strong>
      <p className={styles.cardText}>
        {recurrenceText(s)} · {s.target} · {s.mode} mode
      </p>
      <p className={styles.cardText}>{s.instructions}</p>
      {p.warning && p.status === "proposed" && <p className={styles.error}>{p.warning}</p>}
      {p.status === "accepted" && (
        <p className={styles.done}>
          Accepted ·{" "}
          <button type="button" className={styles.link} onClick={() => openSchedules({ kind: "schedule", id: s.id })}>
            see it in Schedules
          </button>
        </p>
      )}
      {p.status === "refused" && <p className={styles.refused}>Refused{p.refusal_reason ? `: ${p.refusal_reason}` : ""}</p>}
      {p.status === "proposed" &&
        (refusing ? (
          <div className={styles.actions}>
            <input
              className={styles.input}
              placeholder="Reason, passed on to the director"
              aria-label="Reason for refusing"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <button type="button" className={styles.btn} disabled={busy} onClick={() => void run(() => refuseScheduleProposal(p.id, reason), onDecided)}>
              Refuse
            </button>
            <button type="button" className={styles.link} onClick={() => setRefusing(false)}>
              Cancel
            </button>
          </div>
        ) : (
          <div className={styles.actions}>
            <button type="button" className={styles.btnPrimary} disabled={busy} onClick={() => void run(() => acceptScheduleProposal(p.id), onDecided)}>
              Accept
            </button>
            <button type="button" className={styles.btn} disabled={busy} onClick={() => openSchedules({ kind: "proposal", id: p.id })}>
              Edit…
            </button>
            <button type="button" className={styles.btn} disabled={busy} onClick={() => setRefusing(true)}>
              Refuse…
            </button>
          </div>
        ))}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

const DESTINATIONS: Record<string, string> = {
  session: "this session",
  localSettings: "this project, locally",
  projectSettings: "this project, shared",
  userSettings: "all your projects",
};

// describeSuggestion is the label of an "allow and…" button: what Claude Code
// offers with its prompt, and where the change would be kept.
export function describeSuggestion(s: PermissionUpdate): string {
  const where = s.destination ? ` (${DESTINATIONS[s.destination] ?? s.destination})` : "";
  const rules = (s.rules ?? []).map((r) => (r.ruleContent ? `${r.toolName}(${r.ruleContent})` : r.toolName)).join(", ");
  switch (s.type) {
    case "setMode":
      return `Allow and switch to ${s.mode} mode${where}`;
    case "addRules":
      return `Allow and always ${s.behavior === "deny" ? "deny" : s.behavior === "ask" ? "ask for" : "allow"} ${rules}${where}`;
    case "addDirectories":
      return `Allow and add ${(s.directories ?? []).join(", ")}${where}`;
    default:
      return `Allow and apply ${s.type}${where}`;
  }
}
