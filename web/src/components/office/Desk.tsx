import type { Agent, BudgetGauge } from "@/lib/api";
import { gaugeText, progress, tightest } from "@/lib/budgets";
import { lookOf } from "@/lib/look";
import { isDirector, statusLabel } from "../chat/agents";
import { Dialogue } from "./Dialogue";
import type { Talking } from "./speech";
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
  // Away at the library (a knowledge tool, ADR-0019): its chair is empty
  // until its walker comes back.
  reading?: boolean;
  // What the agent says to its teammates (speech.ts): speech bubbles in
  // place of its status bubble while it talks. Whether they pop in.
  talking?: Talking;
  animate?: boolean;
  // The budgets that cover the agent (ADR-0013): the gauge shows the one
  // closest to its threshold, the tooltip lists them all.
  gauges: BudgetGauge[];
  onSelect: () => void;
}

// Desk is an agent at work: the character behind its desk, a bubble telling
// its status, its name plate, and a small gauge of its tightest budget. An
// agent on leave left an empty chair and a note; one on budget hold shows an
// empty wallet.
export function Desk({ agent, at, ticket, selected, arriving, reading = false, talking, animate = true, gauges, onSelect }: Props) {
  const away = agent.on_leave;
  const off = agent.status === "stopped" || agent.status === "exited";
  const hidden = arriving || reading;
  // Nobody talks from an empty chair, or before it has sat down.
  const speaking = !away && !arriving && talking !== undefined && talking.bubbles.length > 0;

  return (
    <button
      type="button"
      className={styles.desk}
      style={{ left: at.x, top: at.y }}
      data-address={agent.address}
      data-status={away ? "on_leave" : agent.status}
      data-talking={speaking}
      aria-pressed={selected}
      aria-label={`${agent.name}, ${agent.role || "agent"}, ${statusLabel(agent)}`}
      onClick={onSelect}
    >
      {away ? (
        <span className={styles.chair} />
      ) : (
        <>
          <span className={styles.char} data-off={off} style={{ visibility: hidden ? "hidden" : undefined }}>
            <Sprite look={lookOf(agent.address, isDirector(agent))} />
          </span>
          {!hidden && !speaking && <Bubble status={agent.status} />}
        </>
      )}
      {speaking && talking && <Dialogue talking={talking} animate={animate} />}
      <span className={styles.deskTop}>
        <span className={styles.laptop} />
        <span className={styles.mug} />
      </span>
      {away && <span className={styles.note}>ON LEAVE</span>}
      <span className={styles.plate}>
        {agent.name} <em>{away ? "away" : reading ? "library" : (ticket ?? (isDirector(agent) ? "boss" : off ? "off" : ""))}</em>
      </span>
      <MiniGauge gauges={gauges} className={styles.deskGauge} />
    </button>
  );
}

// MiniGauge fills up to the spend of the tightest of the gauges given; it
// turns amber near the threshold and red at it. Nothing without a gauge.
export function MiniGauge({ gauges, className }: { gauges: BudgetGauge[]; className: string }) {
  const tight = tightest(gauges);
  if (!tight) return null;

  const p = progress(tight);
  const ratio = tight.limit_usd > 0 ? tight.spent_usd / tight.limit_usd : 0;

  return (
    <span
      className={`${styles.miniGauge} ${className}`}
      data-tone={p >= 1 ? "held" : p >= 0.75 ? "near" : "ok"}
      title={gauges.map(gaugeText).join("\n")}
    >
      <span style={{ width: `${Math.min(ratio, 1) * 100}%` }} />
    </span>
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
    case "budget_hold":
      return (
        <span className={`${styles.say} ${styles.sayWallet}`} aria-hidden="true">
          <i />
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
