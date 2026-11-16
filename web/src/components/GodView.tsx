"use client";

import { type FormEvent, useState } from "react";

import { type Agent, type Feature, HUMAN, type Message, listFeatures, sendMessage } from "@/lib/api";
import { usePolling } from "@/lib/usePolling";
import styles from "./ControlRoom.module.css";
import { ProposalCard } from "./ProposalCard";

interface Props {
  director: Agent | null;
  messages: Message[];
  onChanged: () => void;
}

const FEATURES_REFRESH_MS = 3000;

// Acts the human answers with in_reply_to: the director expects an answer.
const EXPECTS_ANSWER = new Set(["request", "query", "propose"]);

// GodView is the conversation between the human and the director (docs/god.md):
// the messages between them, oldest first, the director's feature proposals
// with Start / Refuse, and a box to write to it. Deliberately plain: the real
// design pass comes later.
export function GodView({ director, messages, onChanged }: Props) {
  const features = usePolling(listFeatures, FEATURES_REFRESH_MS);

  if (!director) {
    return (
      <p className={styles.empty}>
        No director yet. Recruit one from the Recruitment tab: pick the <code>god</code> template and choose &quot;the director (no
        team)&quot;.
      </p>
    );
  }

  const god = director.address;
  const conversation = messages.filter((m) => (m.from === god && m.to === HUMAN) || (m.from === HUMAN && m.to === god));

  return (
    <div className={styles.tickets}>
      <p className={styles.muted}>
        {god} — {director.role} · {director.on_leave ? "on leave" : director.status}
      </p>
      <ol className={styles.feed}>
        {conversation.length === 0 && <li className={styles.empty}>Tell {god} what you need.</li>}
        {conversation.map((m) => (
          <GodMessage
            key={m.id}
            message={m}
            god={god}
            features={features.data ?? []}
            onChanged={() => {
              void features.refresh();
              onChanged();
            }}
          />
        ))}
      </ol>
      <GodComposer god={god} lastQuestion={lastQuestion(conversation, god)} onSent={onChanged} />
    </div>
  );
}

function GodMessage({ message, god, features, onChanged }: { message: Message; god: string; features: Feature[]; onChanged: () => void }) {
  const fromGod = message.from === god;
  const proposal = fromGod && message.act === "propose" ? features.find((f) => f.proposal_message === message.id) : undefined;

  return (
    <li className={fromGod ? styles.message : styles.messageMine}>
      <header className={styles.messageHeader}>
        <span className={styles.route}>{fromGod ? god : "you"}</span>
        <span className={styles.act}>{message.act}</span>
        <time className={styles.time} dateTime={message.sent_at}>
          {new Date(message.sent_at).toLocaleTimeString()}
        </time>
      </header>
      <p className={styles.subject}>{message.subject}</p>
      {message.body && <p className={styles.body}>{message.body}</p>}
      {proposal?.status === "proposed" && <ProposalCard feature={proposal} onDecided={onChanged} />}
      {proposal && proposal.status !== "proposed" && <p className={styles.muted}>Feature {proposal.status}.</p>}
    </li>
  );
}

// GodComposer writes to the director. While it waits for an answer (its
// latest request or question), the message answers it.
function GodComposer({ god, lastQuestion, onSent }: { god: string; lastQuestion: Message | null; onSent: () => void }) {
  const [body, setBody] = useState("");
  const [answering, setAnswering] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const replyTo = answering ? lastQuestion : null;

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      await sendMessage({
        to: god,
        act: replyTo ? "inform" : "request",
        priority: "normal",
        subject: replyTo ? `Re: ${replyTo.subject}` : firstLine(body),
        body,
        in_reply_to: replyTo?.id,
      });
      setBody("");
      setError(null);
      onSent();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <form className={styles.composer} onSubmit={submit}>
      {lastQuestion && (
        <label className={styles.checkbox}>
          <input type="checkbox" checked={answering} onChange={(e) => setAnswering(e.target.checked)} /> answer &quot;
          {lastQuestion.subject}&quot;
        </label>
      )}
      <textarea rows={4} placeholder={`Write to ${god}…`} value={body} onChange={(e) => setBody(e.target.value)} />
      <div className={styles.row}>
        <button type="submit" className={styles.primaryButton} disabled={!body.trim()}>
          Send
        </button>
        {error && <span className={styles.error}>{error}</span>}
      </div>
    </form>
  );
}

// lastQuestion is the director's latest message expecting an answer, if the
// human has not written since.
function lastQuestion(conversation: Message[], god: string): Message | null {
  const last = conversation[conversation.length - 1];
  return last && last.from === god && EXPECTS_ANSWER.has(last.act) && last.act !== "propose" ? last : null;
}

function firstLine(text: string): string {
  const line = text.trim().split("\n")[0] ?? "";
  return line.length > 80 ? `${line.slice(0, 77)}…` : line;
}
