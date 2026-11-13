"use client";

import { useEffect, useState } from "react";

import {
  type Agent,
  type AgentDefinition,
  dismissAgent,
  editDefinition,
  getDefinition,
  putOnLeave,
  returnFromLeave,
} from "@/lib/api";
import styles from "./ControlRoom.module.css";
import { DefinitionEditor, cleanDefinition } from "./DefinitionEditor";

interface Props {
  agent: Agent;
  onChanged: () => void;
}

// AgentEditor edits a recruited agent: its definition (the agent restarts with
// it once idle), leave and return, dismissal. Changes never touch the
// template it came from.
export function AgentEditor({ agent, onChanged }: Props) {
  const [definition, setDefinition] = useState<AgentDefinition | null>(null);
  const [fresh, setFresh] = useState(false);
  const [now, setNow] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let current = true;
    getDefinition(agent.address)
      .then((d) => current && setDefinition(d))
      .catch((e: unknown) => current && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      current = false;
    };
  }, [agent.address]);

  async function act(action: () => Promise<unknown>, done: string) {
    try {
      await action();
      setError(null);
      setNotice(done);
      onChanged();
    } catch (e) {
      setNotice(null);
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  const whenApplied = now ? "now" : "once its current turn is over";

  return (
    <section className={styles.ticketDetail}>
      <h3 className={styles.subject}>
        {agent.address} <span className={styles.muted}>{agent.from ? `recruited from ${agent.from}` : "created directly"}</span>
      </h3>
      <p className={styles.muted}>
        {agent.on_leave ? "On leave." : `Status: ${agent.status}.`}
        {agent.pending_change && " A change waits for the end of its turn."}
      </p>

      {definition && <DefinitionEditor value={definition} onChange={setDefinition} />}

      <div className={styles.row}>
        <button
          type="button"
          className={styles.primaryButton}
          disabled={!definition}
          onClick={() =>
            definition &&
            act(() => editDefinition(agent.address, cleanDefinition(definition), { fresh, now }), `Saved: the agent restarts ${whenApplied}.`)
          }
        >
          Save and restart
        </button>
        <label className={styles.checkbox} title="Start a new Claude Code session: the agent forgets everything">
          <input type="checkbox" checked={fresh} onChange={(e) => setFresh(e.target.checked)} /> new session
        </label>
        <label className={styles.checkbox} title="Do not wait for the end of its current turn (e.g. a stuck agent)">
          <input type="checkbox" checked={now} onChange={(e) => setNow(e.target.checked)} /> apply now
        </label>
      </div>

      <div className={styles.row}>
        {agent.on_leave ? (
          <button type="button" className={styles.smallButton} onClick={() => act(() => returnFromLeave(agent.address, { fresh }), "Back from leave.")}>
            Return from leave
          </button>
        ) : (
          <button
            type="button"
            className={styles.smallButton}
            onClick={() => act(() => putOnLeave(agent.address, { now }), `On leave ${whenApplied}.`)}
          >
            Put on leave
          </button>
        )}
        <button
          type="button"
          className={styles.denyButton}
          onClick={() => {
            if (window.confirm(`Dismiss ${agent.address}? Its file is deleted; its ticket goes back to the team's queue.`)) {
              void act(() => dismissAgent(agent.address, { now }), `Dismissed ${whenApplied}.`);
            }
          }}
        >
          Dismiss
        </button>
      </div>

      {notice && <p className={styles.muted}>{notice}</p>}
      {error && <p className={styles.error}>{error}</p>}
    </section>
  );
}
