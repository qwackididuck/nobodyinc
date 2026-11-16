import type { Agent } from "@/lib/api";
import { lookOf } from "@/lib/look";
import { isDirector, statusLabel } from "../chat/agents";
import type { Point } from "./layout";
import styles from "./office.module.css";
import { Sprite } from "./sprite";

interface Props {
  agent: Agent;
  at: Point;
  // The ticket the agent works on, shown on its name plate.
  ticket?: string;
  selected: boolean;
  // While its walker comes to sit down, the seated character is hidden.
  arriving: boolean;
  onSelect: () => void;
}

// Desk is an agent at work: the character behind its desk, a bubble telling
// its status, and its name plate. An agent on leave left an empty chair and
// a note.
export function Desk({ agent, at, ticket, selected, arriving, onSelect }: Props) {
  const away = agent.on_leave;
  const off = agent.status === "stopped" || agent.status === "exited";

  return (
    <button
      type="button"
      className={styles.desk}
      style={{ left: at.x, top: at.y }}
      data-address={agent.address}
      data-status={away ? "on_leave" : agent.status}
      aria-pressed={selected}
      aria-label={`${agent.name}, ${agent.role || "agent"}, ${statusLabel(agent)}`}
      onClick={onSelect}
    >
      {away ? (
        <span className={styles.chair} />
      ) : (
        <>
          <span className={styles.char} data-off={off} style={{ visibility: arriving ? "hidden" : undefined }}>
            <Sprite look={lookOf(agent.address, isDirector(agent))} />
          </span>
          {!arriving && <Bubble status={agent.status} />}
        </>
      )}
      <span className={styles.deskTop}>
        <span className={styles.laptop} />
        <span className={styles.mug} />
      </span>
      {away && <span className={styles.note}>ON LEAVE</span>}
      <span className={styles.plate}>
        {agent.name} <em>{away ? "away" : (ticket ?? (isDirector(agent) ? "boss" : off ? "off" : ""))}</em>
      </span>
    </button>
  );
}

function Bubble({ status }: { status: Agent["status"] }) {
  switch (status) {
    case "working":
    case "starting":
      return (
        <span className={`${styles.say} ${styles.sayWorking}`} aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
      );
    case "awaiting_approval":
      return (
        <span className={`${styles.say} ${styles.sayApproval}`} aria-hidden="true">
          !
        </span>
      );
    case "idle":
      return (
        <span className={`${styles.say} ${styles.sayIdle}`} aria-hidden="true">
          z
        </span>
      );
    default:
      return (
        <span className={styles.say} aria-hidden="true">
          OFF
        </span>
      );
  }
}
