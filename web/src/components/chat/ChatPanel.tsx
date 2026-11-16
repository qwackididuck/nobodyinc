"use client";

import { useLive } from "@/lib/live";
import type { ChatTarget } from "../App";
import { isDirector, sortAgents } from "./agents";
import { AvatarRail, BIN_KEY } from "./AvatarRail";
import { Bin } from "./Bin";
import { HireDirector } from "./HireDirector";
import styles from "./chat.module.css";
import { useReadMarks } from "./readMarks";
import { Thread } from "./Thread";

interface Props {
  target: ChatTarget | null;
  onTarget: (t: ChatTarget) => void;
}

// ChatPanel is the left side of the control room: the avatar rail and the
// selected chat (the director's by default) or the wastebasket.
export function ChatPanel({ target, onTarget }: Props) {
  const { agents, teams, settings } = useLive();
  const { marks, markSeen } = useReadMarks(settings?.project_id);
  const sorted = sortAgents(agents, teams);

  // Until the human picks a chat, show the first one (the director's). Once
  // the director is recruited, its chat replaces the recruiting card.
  const director = sorted.find(isDirector);
  const first: ChatTarget | null = sorted[0] ? { kind: "agent", address: sorted[0].address } : null;
  const shown: ChatTarget | null =
    target?.kind === "hire-director" && director ? { kind: "agent", address: director.address } : (target ?? first);
  const agent = shown?.kind === "agent" ? sorted.find((a) => a.address === shown.address) : undefined;

  return (
    <>
      <AvatarRail agents={sorted} target={shown} marks={marks} onTarget={onTarget} />
      <aside className={styles.panel} aria-label="Conversation">
        {shown?.kind === "bin" && <Bin onSeen={(at) => markSeen(BIN_KEY, at)} />}
        {shown?.kind === "hire-director" && <HireDirector onHired={(address) => onTarget({ kind: "agent", address })} />}
        {agent && <Thread key={agent.address} agent={agent} onSeen={(at) => markSeen(agent.address, at)} />}
        {!shown && <p className={styles.empty}>No agent yet.</p>}
        {shown?.kind === "agent" && !agent && <p className={styles.empty}>This agent left the organization.</p>}
      </aside>
    </>
  );
}
