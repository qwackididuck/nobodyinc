"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { type Agent, type Approval, HUMAN, type Message, listConversation, startAgent, terminalURL } from "@/lib/api";
import { useLive } from "@/lib/live";
import { TerminalView } from "./TerminalView";
import { isAnswered, isDirector, statusKey, statusLabel, teamLabel, timeOf, withHuman } from "./agents";
import { Avatar } from "./Avatar";
import { ApprovalCard, BudgetProposalCard, BudgetRequestCard, FeatureProposal, ScheduleProposalCard, UrgentQuestion } from "./cards";
import styles from "./chat.module.css";
import { Composer } from "./Composer";

interface Props {
  agent: Agent;
  // cutoff: messages sent before this time are hidden until asked for.
  cutoff: string;
  // onSeen records the time of the latest message shown, as read.
  onSeen: (at: string) => void;
}

// PAGE is how many hidden messages "show earlier" reveals at a time.
const PAGE = 25;

type Item = { at: string; message: Message } | { at: string; approval: Approval };

// Thread is the human's chat with one agent: the messages both ways, with
// cards for what waits for the human (a permission request, an urgent
// question, a feature or schedule proposal), and a switch to the agent's live terminal.
export function Thread({ agent, cutoff, onSeen }: Props) {
  const live = useLive();
  const [view, setView] = useState<"chat" | "terminal">("chat");
  // The terminal fills the whole window while expanded; Escape brings it back.
  const [expanded, setExpanded] = useState(false);
  const [history, setHistory] = useState<Message[]>([]);
  const [replyTo, setReplyTo] = useState<Message | null>(null);
  const [error, setError] = useState<string | null>(null);
  // How many of the hidden (older than the cutoff) messages are shown, newest first.
  // Tied to its cutoff: a new cutoff (a feature was finished) hides the history again.
  const [reveal, setReveal] = useState({ cutoff, count: 0 });
  const revealed = reveal.cutoff === cutoff ? reveal.count : 0;
  const listRef = useRef<HTMLOListElement>(null);
  const stuck = useRef(true); // the list follows its end until the human scrolls up

  // The full history comes from the API once; new messages come with the
  // event stream (live.messages).
  useEffect(() => {
    let cancelled = false;
    listConversation(agent.address)
      .then((ms) => !cancelled && setHistory(ms))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, [agent.address]);

  const messages = mergeById(history, live.messages.filter((m) => withHuman(m, agent.address)));
  const firstShown = messages.findIndex((m) => m.sent_at >= cutoff);
  const afterCount = firstShown === -1 ? 0 : messages.length - firstShown;
  const visible = messages.slice(Math.max(0, messages.length - afterCount - revealed));
  const stillHidden = messages.length - visible.length;
  const items: Item[] = [
    ...visible.map((m) => ({ at: m.sent_at, message: m })),
    ...live.approvals.filter((a) => a.agent === agent.address).map((a) => ({ at: a.created_at, approval: a })),
  ].sort((a, b) => a.at.localeCompare(b.at));

  const latest = messages.at(-1)?.sent_at;
  useEffect(() => {
    if (latest && view === "chat") onSeen(latest);
  }, [latest, view, onSeen]);

  // Open on the last message, and follow new ones unless the human scrolled up.
  const lastItem = items.at(-1)?.at;
  useLayoutEffect(() => {
    const el = listRef.current;
    if (el && stuck.current) el.scrollTop = el.scrollHeight;
  }, [lastItem, items.length, view, history.length]);

  useEffect(() => {
    if (!expanded) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setExpanded(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [expanded]);

  const canStart = !agent.on_leave && (agent.status === "stopped" || agent.status === "exited");

  async function start() {
    try {
      await startAgent(agent.address);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <>
      <header className={styles.head}>
        <Avatar agent={agent} size="lg" />
        <div className={styles.who}>
          <strong className={styles.name}>{agent.name}</strong>
          <span className={styles.role}>
            {agent.role || (isDirector(agent) ? "Director" : "Agent")} · {teamLabel(agent)}
          </span>
        </div>
        <span className={styles.pill} data-status={statusKey(agent)}>
          {statusLabel(agent)}
        </span>
      </header>
      {canStart && (
        <div className={styles.notice}>
          <span>{agent.name} is not running.</span>
          <button type="button" className={styles.btn} onClick={() => void start()}>
            Start
          </button>
        </div>
      )}
      <div className={styles.toggle} role="tablist" aria-label="Show">
        {(["chat", "terminal"] as const).map((v) => (
          <button key={v} type="button" role="tab" aria-selected={view === v} className={styles.toggleBtn} onClick={() => setView(v)}>
            {v === "chat" ? "Chat" : "Terminal"}
          </button>
        ))}
        {view === "terminal" && (
          <button type="button" className={styles.toggleBtn} onClick={() => setExpanded((e) => !e)} aria-pressed={expanded}>
            {expanded ? "Reduce (Esc)" : "Expand"}
          </button>
        )}
      </div>
      {view === "terminal" ? (
        <div className={expanded ? `${styles.terminal} ${styles.terminalExpanded}` : styles.terminal}>
          {expanded && (
            <button type="button" className={`${styles.btn} ${styles.terminalReduce}`} onClick={() => setExpanded(false)}>
              Reduce (Esc)
            </button>
          )}
          <TerminalView url={terminalURL(agent.address)} status={agent.status} />
        </div>
      ) : (
        <>
          <ol
            ref={listRef}
            onScroll={(e) => {
              const el = e.currentTarget;
              stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
            }}
            className={styles.body}
            aria-label={`Chat with ${agent.name}`}>
            {stillHidden > 0 && (
              <li className={styles.system}>
                <button
                  type="button"
                  className={styles.btn}
                  onClick={() => {
                    stuck.current = false;
                    setReveal({ cutoff, count: revealed + PAGE });
                  }}
                >
                  Show {Math.min(PAGE, stillHidden)} earlier ({stillHidden} hidden)
                </button>
              </li>
            )}
            {items.length === 0 && <li className={styles.system}>No message with {agent.name} yet.</li>}
            {items.map((item) =>
              "approval" in item ? (
                <li key={item.approval.id} className={styles.in}>
                  <ApprovalCard approval={item.approval} onDecided={() => live.reload("approvals")} />
                </li>
              ) : (
                <MessageItem key={item.message.id} message={item.message} agent={agent} messages={messages} onReply={setReplyTo} />
              ),
            )}
          </ol>
          {error && <p className={styles.error}>{error}</p>}
          <Composer agent={agent} replyTo={replyTo} onCancelReply={() => setReplyTo(null)} />
        </>
      )}
    </>
  );
}

interface MessageItemProps {
  message: Message;
  agent: Agent;
  messages: Message[];
  onReply: (m: Message) => void;
}

// MessageItem shows one message: a bubble, or a card when it waits for the
// human (an unanswered urgent question, a pending feature proposal).
function MessageItem({ message: m, agent, messages, onReply }: MessageItemProps) {
  const { features, scheduleProposals, budgetRequests, budgetProposals, reload } = useLive();
  const out = m.from === HUMAN;

  // The director's budget cards (ADR-0013): an extension request it passed
  // on, or a cap it proposes.
  const budgetRequest = m.act === "propose" ? budgetRequests.find((r) => r.card_message === m.id) : undefined;
  if (budgetRequest) {
    return (
      <li className={styles.in}>
        <BudgetRequestCard request={budgetRequest} onDecided={() => reload("budgetRequests")} />
        <span className={styles.time}>{timeOf(m.sent_at)}</span>
      </li>
    );
  }

  const budgetProposal = m.act === "propose" ? budgetProposals.find((p) => p.proposal_message === m.id) : undefined;
  if (budgetProposal) {
    return (
      <li className={styles.in}>
        <BudgetProposalCard proposal={budgetProposal} onDecided={() => reload("budgetProposals")} />
        <span className={styles.time}>{timeOf(m.sent_at)}</span>
      </li>
    );
  }

  const scheduleProposal = m.act === "propose" ? scheduleProposals.find((p) => p.proposal_message === m.id) : undefined;
  if (scheduleProposal) {
    return (
      <li className={styles.in}>
        <ScheduleProposalCard proposal={scheduleProposal} onDecided={() => reload("scheduleProposals")} />
        <span className={styles.time}>{timeOf(m.sent_at)}</span>
      </li>
    );
  }

  const proposal = m.act === "propose" ? features.find((f) => f.proposal_message === m.id) : undefined;
  if (proposal) {
    return (
      <li className={styles.in}>
        <FeatureProposal feature={proposal} onDecided={() => reload("features")} />
        <span className={styles.time}>{timeOf(m.sent_at)}</span>
      </li>
    );
  }

  if (!out && m.priority === "urgent" && !isAnswered(m, messages)) {
    return (
      <li className={styles.in}>
        <UrgentQuestion message={m} agent={agent} />
        <span className={styles.time}>{timeOf(m.sent_at)}</span>
      </li>
    );
  }

  const showSubject = m.subject && !m.body.startsWith(m.subject) && !m.subject.startsWith("Re: ");

  return (
    <li className={out ? styles.out : styles.in}>
      <div className={styles.bubble}>
        {showSubject && <strong className={styles.subject}>{m.subject}</strong>}
        <p className={styles.text}>{m.body || m.subject}</p>
      </div>
      <span className={styles.time}>
        {m.priority === "urgent" && <span className={styles.tag}>urgent</span>}
        {m.act !== "request" && m.act !== "inform" && <span className={styles.tag}>{m.act}</span>}
        {timeOf(m.sent_at)}
        {out && <span title={m.delivered_at ? "Delivered" : "Waiting in the agent's tray"}>{m.delivered_at ? " ✓" : " ·"}</span>}
        {!out && (
          <button type="button" className={styles.link} onClick={() => onReply(m)}>
            Reply
          </button>
        )}
      </span>
    </li>
  );
}

// mergeById merges two lists of messages without duplicates, oldest first.
function mergeById(a: Message[], b: Message[]): Message[] {
  const byId = new Map<string, Message>();
  for (const m of [...a, ...b]) byId.set(m.id, { ...byId.get(m.id), ...m });
  return [...byId.values()].sort((x, y) => x.sent_at.localeCompare(y.sent_at));
}
