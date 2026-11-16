"use client";

import { type FormEvent, type KeyboardEvent, useState } from "react";

import { type Agent, type Message, sendMessage } from "@/lib/api";
import styles from "./chat.module.css";

interface Props {
  agent: Agent;
  replyTo: Message | null;
  onCancelReply: () => void;
}

const SUBJECT_CHARS = 60;

// Composer writes to the agent of the chat. Enter sends, Shift+Enter starts a
// new line. A new message is a request; a reply informs, as an answer to the
// message it replies to. The subject is the first line, shortened.
export function Composer({ agent, replyTo, onCancelReply }: Props) {
  const [body, setBody] = useState("");
  const [urgent, setUrgent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function send() {
    const text = body.trim();
    if (!text) return;
    setBusy(true);
    try {
      await sendMessage({
        to: agent.address,
        act: replyTo ? "inform" : "request",
        priority: urgent ? "urgent" : "normal",
        subject: replyTo ? `Re: ${replyTo.subject}` : subjectOf(text),
        body: text,
        in_reply_to: replyTo?.id,
      });
      setBody("");
      setUrgent(false);
      setError(null);
      onCancelReply();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    void send();
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <form className={styles.composer} onSubmit={submit}>
      {replyTo && (
        <p className={styles.replying}>
          Replying to <em>{replyTo.subject || replyTo.body.slice(0, SUBJECT_CHARS)}</em>
          <button type="button" className={styles.link} onClick={onCancelReply}>
            Cancel
          </button>
        </p>
      )}
      <div className={styles.composerRow}>
        <textarea
          id="composer-input"
          className={styles.composerInput}
          rows={1}
          placeholder={agent.on_leave ? `Leave a message for ${agent.name}…` : `Message ${agent.name}…`}
          aria-label={`Message ${agent.name}`}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={onKeyDown}
        />
        <button type="submit" className={styles.send} disabled={busy || !body.trim()}>
          Send
        </button>
      </div>
      <label className={styles.urgent}>
        <input type="checkbox" checked={urgent} onChange={(e) => setUrgent(e.target.checked)} /> Urgent: answered at once, even mid-task
      </label>
      {error && <p className={styles.error}>{error}</p>}
    </form>
  );
}

function subjectOf(text: string): string {
  const first = text.split("\n")[0].trim();
  return first.length > SUBJECT_CHARS ? `${first.slice(0, SUBJECT_CHARS - 1)}…` : first;
}
