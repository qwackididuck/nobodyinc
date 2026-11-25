"use client";

import { useLive } from "@/lib/live";
import type { ChatTarget } from "../App";
import { sortAgents } from "./agents";
import { AvatarRail, BIN_KEY } from "./AvatarRail";
import { Bin } from "./Bin";
import styles from "./chat.module.css";
import type { ReadMarks } from "./readMarks";
import { Thread } from "./Thread";
import { ResizeHandle } from "./ResizeHandle";
import { useHistoryCutoff } from "./useHistoryCutoff";
import type { useChatWidth } from "./useChatWidth";

interface Props {
  target: ChatTarget | null;
  onTarget: (t: ChatTarget) => void;
  marks: ReadMarks;
  markSeen: (chat: string, at: string) => void;
  width: ReturnType<typeof useChatWidth>;
}

// ChatPanel is the left side of the control room: the avatar rail and the
// selected chat (the director's by default) or the wastebasket.
export function ChatPanel({ target, onTarget, marks, markSeen, width }: Props) {
  const { agents, teams } = useLive();
  const cutoff = useHistoryCutoff();
  const sorted = sortAgents(agents, teams);

  // Until the human picks a chat, show the first one (the director's).
  const first: ChatTarget | null = sorted[0] ? { kind: "agent", address: sorted[0].address } : null;
  const shown: ChatTarget | null = target ?? first;
  const agent = shown?.kind === "agent" ? sorted.find((a) => a.address === shown.address) : undefined;

  return (
    <>
      <AvatarRail agents={sorted} target={shown} marks={marks} onTarget={onTarget} />
      <aside className={styles.panel} aria-label="Conversation">
        {shown?.kind === "bin" && <Bin onSeen={(at) => markSeen(BIN_KEY, at)} />}
        {agent && <Thread key={agent.address} agent={agent} cutoff={cutoff} onSeen={(at) => markSeen(agent.address, at)} />}
        {!shown && <p className={styles.empty}>No agent yet.</p>}
        {shown?.kind === "agent" && !agent && <p className={styles.empty}>This agent left the organization.</p>}
        <ResizeHandle width={width.width} onResize={width.setWidth} onDone={width.remember} />
      </aside>
    </>
  );
}
