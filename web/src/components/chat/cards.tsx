"use client";

import { type FormEvent, useState } from "react";

import {
  acceptScheduleProposal,
  type Agent,
  type Approval,
  decideApproval,
  type Feature,
  type Message,
  refuseProposedFeature,
  refuseScheduleProposal,
  type ScheduleProposal,
  sendMessage,
  startProposedFeature,
} from "@/lib/api";
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
            <button type="button" className={styles.btnPrimary} disabled={busy} onClick={() => void run(() => startProposedFeature(feature.id), onDecided)}>
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
