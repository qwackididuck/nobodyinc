"use client";

import { useEffect, useState } from "react";

import { emptyWastebasket, listAgentExchanges, type Message } from "@/lib/api";
import { useDaemonEvents, useLive } from "@/lib/live";
import { betweenAgents, timeOf } from "./agents";
import { BinIcon } from "./Avatar";
import styles from "./chat.module.css";

// Bin is the wastebasket: the messages agents sent each other, once
// delivered, shown as crumpled sheets of paper unfolded again (the paper
// planes of the office end up here). Read-only: the chat is between the
// human and one agent, and an agent's full story is in its terminal.
//
// "Empty" deletes the finished conversations for good (every message
// delivered, the last one expecting no answer); open ones stay, unless the
// human ticks "Also close the open conversations": those are deleted too
// and the agents that waited for an answer are told.
export function Bin({ onSeen }: { onSeen: (at: string) => void }) {
  const live = useLive();
  const [history, setHistory] = useState<Message[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [includeOpen, setIncludeOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [report, setReport] = useState<string | null>(null);

  // Emptied elsewhere (another tab): forget the deleted sheets.
  useDaemonEvents((e) => {
    if (e.type !== "messages.deleted") return;
    const ids = new Set(e.data.ids);
    setHistory((h) => h.filter((m) => !ids.has(m.id)));
  });

  async function empty() {
    setBusy(true);
    try {
      const ids = await emptyWastebasket(includeOpen);
      const gone = new Set(ids);
      setHistory((h) => h.filter((m) => !gone.has(m.id)));
      setReport(
        ids.length
          ? `${ids.length} messages deleted.`
          : includeOpen
            ? "Nothing to delete."
            : "Nothing finished to delete: every conversation is still open.",
      );
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      setConfirming(false);
      setIncludeOpen(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    listAgentExchanges()
      .then((ms) => !cancelled && setHistory(ms))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  const byId = new Map<string, Message>();
  for (const m of [...history, ...live.messages.filter((m) => betweenAgents(m) && m.delivered_at)]) byId.set(m.id, m);
  const sheets = [...byId.values()].sort((a, b) => a.sent_at.localeCompare(b.sent_at));

  const latest = sheets.at(-1)?.sent_at;
  useEffect(() => {
    if (latest) onSeen(latest);
  }, [latest, onSeen]);

  return (
    <>
      <header className={styles.head}>
        <BinIcon />
        <div className={styles.who}>
          <strong className={styles.name}>Wastebasket</strong>
          <span className={styles.role}>Messages between agents, once delivered</span>
        </div>
        <span className={styles.pill} data-status="idle">
          {sheets.length} sheets
        </span>
      </header>
      <div className={styles.notice}>
        {confirming ? (
          <>
            <span>
              {includeOpen
                ? "Delete every conversation for good? The agents waiting for an answer are told."
                : "Delete the finished conversations for good? Open ones stay."}
            </span>
            <label className={styles.checkLine}>
              <input type="checkbox" checked={includeOpen} onChange={(e) => setIncludeOpen(e.target.checked)} />
              Also close the open conversations
            </label>
            <button type="button" className={styles.btnPrimary} disabled={busy} onClick={() => void empty()}>
              Delete
            </button>
            <button
              type="button"
              className={styles.link}
              onClick={() => {
                setConfirming(false);
                setIncludeOpen(false);
              }}
            >
              Cancel
            </button>
          </>
        ) : (
          <>
            <span>{report ?? "Finished conversations can be deleted."}</span>
            <button type="button" className={styles.btn} disabled={sheets.length === 0} onClick={() => setConfirming(true)}>
              Empty
            </button>
          </>
        )}
      </div>
      <ol className={`${styles.body} ${styles.binBody}`} aria-label="Messages between agents">
        {sheets.length === 0 && <li className={styles.system}>Nothing in the wastebasket yet.</li>}
        {sheets.map((m, i) => (
          <li
            key={m.id}
            className={styles.paper}
            data-reply={m.in_reply_to ? "true" : undefined}
            style={{ ["--tilt" as string]: `${(((i * 7) % 5) - 2) * 0.8}deg` }}
          >
            <div className={styles.paperTop}>
              <span className={styles.paperWho}>
                {m.from} → {m.to}
              </span>
              <span className={styles.paperTime}>{timeOf(m.sent_at)}</span>
            </div>
            {m.subject && <strong className={styles.paperSubject}>{m.subject}</strong>}
            {m.body && <p className={styles.paperText}>{m.body}</p>}
            <span className={styles.paperTag}>
              {m.act}
              {m.ticket ? ` · ${m.ticket}` : ""}
            </span>
          </li>
        ))}
      </ol>
      {error && <p className={styles.error}>{error}</p>}
      <p className={styles.binFoot}>Read-only. What an agent did in full is in its terminal.</p>
    </>
  );
}
