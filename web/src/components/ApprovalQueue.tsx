"use client";

import { useState } from "react";

import { type Approval, decideApproval } from "@/lib/api";
import styles from "./ControlRoom.module.css";

interface Props {
  approvals: Approval[];
  onDecided: () => void;
}

// ApprovalQueue lists the agents' permission requests waiting for the human.
// An agent is blocked until its request is allowed or denied; without a
// decision within 9 minutes, the request falls back to its terminal.
export function ApprovalQueue({ approvals, onDecided }: Props) {
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  if (approvals.length === 0) return null;

  async function decide(a: Approval, allow: boolean) {
    try {
      await decideApproval(a.id, allow, allow ? "" : (reasons[a.id] ?? ""));
      setError(null);
      onDecided();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <section className={styles.approvals} aria-label="Approvals">
      <h2 className={styles.panelTitle}>Waiting for your approval ({approvals.length})</h2>
      {approvals.map((a) => (
        <article key={a.id} className={styles.approval}>
          <header className={styles.messageHeader}>
            <span className={styles.route}>{a.agent}</span>
            <span className={styles.act}>{a.tool_name}</span>
            <time className={styles.time} dateTime={a.created_at}>
              {new Date(a.created_at).toLocaleTimeString()}
            </time>
          </header>
          <pre className={styles.toolInput}>{describe(a)}</pre>
          <div className={styles.row}>
            <button type="button" className={styles.primaryButton} onClick={() => decide(a, true)}>
              Allow
            </button>
            <button type="button" className={styles.denyButton} onClick={() => decide(a, false)}>
              Deny
            </button>
            <input
              className={styles.reason}
              placeholder="Reason (shown to the agent if denied)"
              value={reasons[a.id] ?? ""}
              onChange={(e) => setReasons({ ...reasons, [a.id]: e.target.value })}
            />
          </div>
        </article>
      ))}
      {error && <p className={styles.error}>{error}</p>}
    </section>
  );
}

const MAX_INPUT_CHARS = 800;

// describe shows what the agent wants to do: the command for Bash, the file
// for edits, the raw arguments otherwise.
function describe(a: Approval): string {
  const input = a.tool_input ?? {};
  const command = input["command"];
  const file = input["file_path"];
  let text: string;
  if (typeof command === "string") text = command;
  else if (typeof file === "string") text = `${file}\n\n${JSON.stringify(input, null, 2)}`;
  else text = JSON.stringify(input, null, 2);
  return text.length > MAX_INPUT_CHARS ? `${text.slice(0, MAX_INPUT_CHARS)}…` : text;
}
