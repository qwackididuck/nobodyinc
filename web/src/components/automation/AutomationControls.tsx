"use client";

import { useState } from "react";

import { setAutomation } from "@/lib/api";
import { useLive } from "@/lib/live";
import { AutomationWarning } from "./AutomationWarning";
import { DecisionsModal, unseenDecisions } from "./DecisionsModal";
import styles from "./automation.module.css";

// AutomationButton is the top bar's switch of the automatic mode
// (docs/automation.md). Switching on goes through the warning; switching off
// is immediate. Next to it, the decisions the director took in the human's
// place, whenever there are some, with how many are still to review.
export function AutomationButton() {
  const { automation, decisions } = useLive();
  const [warning, setWarning] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const level = automation?.level ?? "off";
  const unseen = unseenDecisions(decisions).length;

  return (
    <>
      <button
        type="button"
        className={styles.toggle}
        aria-pressed={level !== "off"}
        data-level={level}
        disabled={automation === null}
        onClick={() => (level === "off" ? setWarning(true) : void setAutomation("off"))}
        title={level === "off" ? "Let the agents work without asking you (read the warning first)" : "Back to approving by hand"}
      >
        {level === "nobody" ? "Nobody mode" : level === "auto" ? "Auto" : "Auto: off"}
      </button>
      {decisions.length > 0 && (
        <button
          type="button"
          className={styles.decisionsButton}
          data-unseen={unseen > 0}
          onClick={() => setReviewing(true)}
          title="What the director decided in your place"
        >
          Decisions
          {unseen > 0 && <span className={styles.badge}>{unseen}</span>}
        </button>
      )}
      {warning && <AutomationWarning onClose={() => setWarning(false)} />}
      {reviewing && <DecisionsModal onClose={() => setReviewing(false)} />}
    </>
  );
}

// AutomationBanner stays under the top bar, on every tab, as long as the
// automatic mode is on: it is dangerous enough not to be forgotten.
export function AutomationBanner() {
  const { automation, decisions } = useLive();
  const [reviewing, setReviewing] = useState(false);
  const level = automation?.level ?? "off";
  const unseen = unseenDecisions(decisions).length;

  if (level === "off") return null;

  return (
    <div className={styles.banner} role="alert" data-level={level}>
      <span>
        <b>{level === "nobody" ? "NOBODY MODE" : "AUTOMATIC MODE"}</b>: every permission request is allowed without asking you
        {level === "nobody" ? ", and the director answers the agents' questions in your place" : ""}.
      </span>
      {unseen > 0 && (
        <button type="button" className={styles.bannerLink} onClick={() => setReviewing(true)}>
          {unseen} decision{unseen > 1 ? "s" : ""} to review
        </button>
      )}
      <button type="button" className={styles.bannerOff} onClick={() => void setAutomation("off")}>
        Switch off
      </button>
      {reviewing && <DecisionsModal onClose={() => setReviewing(false)} />}
    </div>
  );
}
