"use client";

import { type Agent, HUMAN } from "@/lib/api";
import { useLive } from "@/lib/live";
import type { ChatTarget } from "../App";
import { isAnswered, isDirector, statusKey, statusLabel, unreadInBin } from "./agents";
import { Avatar, BinIcon } from "./Avatar";
import styles from "./chat.module.css";
import type { ReadMarks } from "./readMarks";

interface Props {
  agents: Agent[]; // sorted, director first
  target: ChatTarget | null;
  marks: ReadMarks;
  onTarget: (t: ChatTarget) => void;
}

// The wastebasket's key in the read marks.
export const BIN_KEY = "~bin";

type Badge = { kind: "approval" | "urgent" | "count"; text: string };

// AvatarRail lists the agents like the chats of a messaging app, the
// director first, with the wastebasket at the bottom. A badge tells what
// waits: a permission request, an unanswered urgent question, or the number
// of messages received since the chat was last opened.
export function AvatarRail({ agents, target, marks, onTarget }: Props) {
  const { approvals, messages } = useLive();

  function badgeOf(a: Agent): Badge | null {
    const pendingApprovals = approvals.filter((p) => p.agent === a.address).length;
    if (pendingApprovals > 0) return { kind: "approval", text: String(pendingApprovals) };

    const toHuman = messages.filter((m) => m.from === a.address && m.to === HUMAN);
    if (toHuman.some((m) => m.priority === "urgent" && !isAnswered(m, messages))) return { kind: "urgent", text: "!" };

    const unread = toHuman.filter((m) => m.sent_at > (marks[a.address] ?? "")).length;
    return unread > 0 ? { kind: "count", text: String(unread) } : null;
  }

  const binUnread = unreadInBin(messages, marks[BIN_KEY]);

  const hasDirector = agents.some(isDirector);

  return (
    <nav className={styles.rail} aria-label="Agents">
      {!hasDirector && (
        <>
          <button
            type="button"
            className={styles.railItem}
            aria-current={target?.kind === "hire-director"}
            title="No director yet: recruit one"
            onClick={() => onTarget({ kind: "hire-director" })}
          >
            <span className={`${styles.avatar} ${styles.avatarGhost}`} aria-hidden="true">
              +
            </span>
            <span className={styles.railName}>director</span>
          </button>
          {agents.length > 0 && <span className={styles.railSep} />}
        </>
      )}
      {agents.map((a, i) => {
        const badge = badgeOf(a);
        const current = target?.kind === "agent" && target.address === a.address;
        return (
          <div key={a.address} className={styles.railGroup}>
            <button
              type="button"
              className={styles.railItem}
              aria-current={current}
              title={`${a.address} · ${a.role} · ${statusLabel(a)}`}
              onClick={() => onTarget({ kind: "agent", address: a.address })}
            >
              <Avatar agent={a} />
              <span className={styles.railDot} data-status={statusKey(a)} />
              {badge && (
                <span className={styles.railBadge} data-kind={badge.kind}>
                  {badge.text}
                </span>
              )}
              <span className={styles.railName}>{a.name}</span>
            </button>
            {i === 0 && isDirector(a) && agents.length > 1 && <span className={styles.railSep} />}
          </div>
        );
      })}
      <span className={`${styles.railSep} ${styles.railSepBottom}`} />
      <button
        type="button"
        className={styles.railItem}
        aria-current={target?.kind === "bin"}
        title="Wastebasket · messages between agents"
        onClick={() => onTarget({ kind: "bin" })}
      >
        <BinIcon />
        {binUnread > 0 && (
          <span className={styles.railBadge} data-kind="count">
            {binUnread}
          </span>
        )}
        <span className={styles.railName}>bin</span>
      </button>
    </nav>
  );
}
