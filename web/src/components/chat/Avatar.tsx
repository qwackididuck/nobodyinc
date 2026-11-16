import type { Agent } from "@/lib/api";
import { avatarColor } from "@/lib/look";
import { isDirector, statusKey } from "./agents";
import styles from "./chat.module.css";

interface Props {
  agent: Agent;
  size?: "md" | "lg";
}

// Avatar is the agent's initial on the color of its pixel character's shirt.
export function Avatar({ agent, size = "md" }: Props) {
  return (
    <span
      className={styles.avatar}
      data-size={size}
      data-status={statusKey(agent)}
      style={{ background: avatarColor(agent.address, isDirector(agent)) }}
      aria-hidden="true"
    >
      {agent.name.slice(0, 1).toUpperCase()}
    </span>
  );
}

// BinIcon is the wastebasket, as an avatar.
export function BinIcon() {
  return (
    <span className={`${styles.avatar} ${styles.avatarBin}`} aria-hidden="true">
      <svg viewBox="0 0 12 12" width="22" height="22" shapeRendering="crispEdges">
        <rect x="2" y="3" width="8" height="8" fill="currentColor" opacity=".55" />
        <rect x="1" y="2" width="10" height="1" fill="currentColor" />
        <rect x="3" y="4" width="1" height="6" fill="currentColor" />
        <rect x="5" y="4" width="1" height="6" fill="currentColor" />
        <rect x="7" y="4" width="1" height="6" fill="currentColor" />
        <rect x="3" y="0" width="3" height="2" fill="#fffaf0" />
        <rect x="6" y="1" width="2" height="1" fill="#e9dfcc" />
      </svg>
    </span>
  );
}
