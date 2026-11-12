"use client";

import { type FormEvent, useState } from "react";

import { ACTS, type Act, type Agent, type Message, sendMessage } from "@/lib/api";
import styles from "./ControlRoom.module.css";

interface Props {
  agents: Agent[];
  defaultTo: string | null;
  replyTo: Message | null;
  onCancelReply: () => void;
  onSent: () => void;
}

// Composer lets the human operator write to any agent, or answer one.
export function Composer({ agents, defaultTo, replyTo, onCancelReply, onSent }: Props) {
  const [to, setTo] = useState<string>("");
  const [act, setAct] = useState<Act>("request");
  const [urgent, setUrgent] = useState(false);
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);

  // A reply goes back to the sender; otherwise to the chosen or focused agent.
  const recipient = replyTo?.from ?? (to || defaultTo || agents[0]?.address || "");

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      await sendMessage({
        to: recipient,
        act,
        priority: urgent ? "urgent" : "normal",
        subject: subject || (replyTo ? `Re: ${replyTo.subject}` : ""),
        body,
        in_reply_to: replyTo?.id,
      });
      setSubject("");
      setBody("");
      setError(null);
      onCancelReply();
      onSent();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <form className={styles.composer} onSubmit={submit}>
      <div className={styles.row}>
        {replyTo ? (
          <span className={styles.replying}>
            Replying to {replyTo.from}{" "}
            <button type="button" className={styles.linkButton} onClick={onCancelReply}>
              cancel
            </button>
          </span>
        ) : (
          <select value={recipient} onChange={(e) => setTo(e.target.value)} aria-label="Recipient">
            {agents.map((a) => (
              <option key={a.address} value={a.address}>
                {a.address}
              </option>
            ))}
          </select>
        )}
        <select value={act} onChange={(e) => setAct(e.target.value as Act)} aria-label="Act">
          {ACTS.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        <label className={styles.checkbox}>
          <input type="checkbox" checked={urgent} onChange={(e) => setUrgent(e.target.checked)} /> urgent
        </label>
      </div>
      <input placeholder="Subject" value={subject} onChange={(e) => setSubject(e.target.value)} />
      <textarea placeholder="Message" rows={3} value={body} onChange={(e) => setBody(e.target.value)} />
      <div className={styles.row}>
        <button type="submit" className={styles.primaryButton} disabled={!recipient}>
          Send as human
        </button>
        {error && <span className={styles.error}>{error}</span>}
      </div>
    </form>
  );
}
