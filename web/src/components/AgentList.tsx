"use client";

import { useState } from "react";

import { type Agent, startAgent } from "@/lib/api";
import styles from "./ControlRoom.module.css";
import { FeaturePanel } from "./FeaturePanel";

interface Props {
  agents: Agent[];
  selected: string | null;
  onSelect: (address: string) => void;
  onChange: () => void;
}

// AgentList shows the organisation team by team, with each agent's status.
export function AgentList({ agents, selected, onSelect, onChange }: Props) {
  const [error, setError] = useState<string | null>(null);
  const teams = groupByTeam(agents);

  async function start(address: string) {
    try {
      await startAgent(address);
      setError(null);
      onChange();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <nav className={styles.panel} aria-label="Agents">
      <FeaturePanel />
      <h2 className={styles.panelTitle}>Teams</h2>
      {[...teams].map(([team, members]) => (
        <section key={team} className={styles.team}>
          <h3 className={styles.teamName}>{team}</h3>
          <ul className={styles.agentList}>
            {members.map((a) => (
              <li key={a.address}>
                <button
                  type="button"
                  className={a.address === selected ? styles.agentSelected : styles.agent}
                  onClick={() => onSelect(a.address)}
                  title="Show this agent's terminal"
                >
                  <span className={styles.agentName}>{a.name}</span>
                  <span className={styles.agentRole}>{a.role}</span>
                  <span className={`${styles.status} ${styles[`status_${a.status}`]}`}>{a.status}</span>
                </button>
                {(a.status === "stopped" || a.status === "exited") && (
                  <button type="button" className={styles.smallButton} onClick={() => start(a.address)}>
                    Start
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
      {error && <p className={styles.error}>{error}</p>}
    </nav>
  );
}

function groupByTeam(agents: Agent[]): Map<string, Agent[]> {
  const teams = new Map<string, Agent[]>();
  for (const a of agents) {
    teams.set(a.team, [...(teams.get(a.team) ?? []), a]);
  }
  return teams;
}
