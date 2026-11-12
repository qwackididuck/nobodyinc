"use client";

import { HUMAN, type Message } from "@/lib/api";
import styles from "./ControlRoom.module.css";

interface Props {
  messages: Message[];
  // When set, only messages sent or received by this address are shown.
  focus: string | null;
  onReply: (message: Message) => void;
}

// MessageFeed lists the mail exchanged in the organisation, oldest first.
export function MessageFeed({ messages, focus, onReply }: Props) {
  const shown = focus ? messages.filter((m) => m.from === focus || m.to === focus) : messages;

  return (
    <ol className={styles.feed}>
      {shown.length === 0 && <li className={styles.empty}>No messages yet.</li>}
      {shown.map((m) => (
        <li key={m.id} className={m.priority === "urgent" ? styles.messageUrgent : styles.message}>
          <header className={styles.messageHeader}>
            <span className={styles.route}>
              {m.from} → {m.to}
            </span>
            <span className={styles.act}>{m.act}</span>
            {m.priority === "urgent" && <span className={styles.urgent}>urgent</span>}
            {m.ticket && <span className={styles.act}>{m.ticket}</span>}
            <time className={styles.time} dateTime={m.sent_at}>
              {new Date(m.sent_at).toLocaleTimeString()}
            </time>
          </header>
          <p className={styles.subject}>{m.subject}</p>
          {m.body && <p className={styles.body}>{m.body}</p>}
          <footer className={styles.messageFooter}>
            <span>{deliveryLabel(m)}</span>
            {m.to === HUMAN && (
              <button type="button" className={styles.smallButton} onClick={() => onReply(m)}>
                Reply
              </button>
            )}
          </footer>
        </li>
      ))}
    </ol>
  );
}

function deliveryLabel(m: Message): string {
  if (m.to === HUMAN) return "for you";
  return m.delivered_at ? `delivered ${new Date(m.delivered_at).toLocaleTimeString()}` : "waiting in tray";
}
