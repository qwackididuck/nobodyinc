"use client";

import { useState } from "react";

import { setAutomation } from "@/lib/api";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./automation.module.css";

interface Props {
  onClose: () => void;
}

// AutomationWarning is the confirmation before the automatic mode is switched
// on (docs/automation.md). It says what the mode allows and what still holds,
// and offers, as a checkbox, the Nobody level on top of it.
export function AutomationWarning({ onClose }: Props) {
  const [nobody, setNobody] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function enable() {
    setBusy(true);
    setError(null);
    try {
      await setAutomation(nobody ? "nobody" : "auto");
      onClose();
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }

  return (
    <Modal label="Switch on the automatic mode" tone="danger" onClose={onClose}>
      <h3 className={ui.title}>Switch on the automatic mode?</h3>
      <p className={styles.lead}>
        Every agent&apos;s permission request will be <b>allowed without you seeing it</b>: shell commands, file edits, MCP tools.
        Nothing restarts, and it applies to all agents at once, now.
      </p>
      <div className={styles.facts}>
        <div className={styles.risk}>
          <h4>NO LONGER ASKED</h4>
          <ul>
            <li>any command an agent decides to run, on your machine, with your accounts</li>
            <li>edits anywhere an agent may write</li>
            <li>tools of every MCP server the agents have</li>
          </ul>
        </div>
        <div className={styles.safe}>
          <h4>STILL REFUSED</h4>
          <ul>
            <li>
              what the agents&apos; <b>deny rules</b> forbid, which Claude Code applies before asking: <code>.nobody/</code>,{" "}
              <code>.claude/</code>, <code>git push</code> under the allowlist profile
            </li>
          </ul>
          <h4>ENDS BY ITSELF</h4>
          <ul>
            <li>when the project is closed or its daemon restarts</li>
          </ul>
        </div>
      </div>
      <label className={styles.nobody}>
        <input type="checkbox" checked={nobody} onChange={(e) => setNobody(e.target.checked)} />
        <span>
          <b>Nobody mode</b>: when an agent asks <i>you</i> a question, the director decides in your place.
          <small>
            Recommended for a long absence. It answers the agent, and reports each decision with its impact, highlighted, for you to
            review when you are back. Proposals (features, schedules, budgets) still wait for you.
          </small>
        </span>
      </label>
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.actions}>
        <button type="button" className={ui.btn} onClick={onClose}>
          Cancel
        </button>
        <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void enable()}>
          {busy ? "Switching on…" : nobody ? "Switch on, Nobody mode" : "Switch on"}
        </button>
      </div>
    </Modal>
  );
}
