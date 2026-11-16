"use client";

import { useState } from "react";

import ui from "../ui/ui.module.css";
import { Catalog } from "./Catalog";
import { EditAgent, NewAgent } from "./Editors";
import styles from "./recruitment.module.css";
import { Staff } from "./Staff";

type View = { kind: "catalog" } | { kind: "staff" } | { kind: "new" } | { kind: "edit"; address: string };

interface Props {
  onOpenChat: (address: string) => void;
}

const TABS: { kind: "catalog" | "staff" | "new"; label: string }[] = [
  { kind: "catalog", label: "Catalog" },
  { kind: "staff", label: "Staff" },
  { kind: "new", label: "New agent" },
];

// Recruitment hires and manages agents: the catalog of templates as a
// character select screen, the staff of the office, and an editor for an
// agent's file (docs/agents.md). Recruits walk into the office; changes
// apply once an agent ends its turn, never in the middle of one.
export function Recruitment({ onOpenChat }: Props) {
  const [view, setView] = useState<View>({ kind: "catalog" });

  const heading = {
    catalog: ["Choose your recruit", "Templates from Nobody Inc and from ~/.nobody/catalog. A recruit is a copy: later changes to the template never touch it."],
    staff: ["Your staff", "Everyone in the office. Changes apply once the agent is idle, unless you ask for now."],
    new: ["New agent", "Writes the agent's file in .nobody/ and starts it, or saves it as a template."],
    edit: ["Edit an agent", "Changing the role or the instructions starts a new session: a resumed session keeps its original instructions."],
  }[view.kind];

  return (
    <div className={`${ui.scroll} ${styles.screen}`}>
      <header className={styles.head}>
        <div>
          <h2 className={ui.title}>
            {view.kind === "edit" ? `Edit ${view.address}` : heading[0]} {view.kind === "catalog" && <span className={styles.blink}>▶</span>}
          </h2>
          <p className={ui.sub}>{heading[1]}</p>
        </div>
        <div className={styles.subtabs} role="tablist" aria-label="Recruitment">
          {TABS.map((t) => (
            <button key={t.kind} type="button" role="tab" aria-selected={view.kind === t.kind} onClick={() => setView({ kind: t.kind })}>
              {t.label}
            </button>
          ))}
        </div>
      </header>
      <div className={styles.body}>
        {view.kind === "catalog" && <Catalog />}
        {view.kind === "staff" && <Staff onOpenChat={onOpenChat} onEdit={(address) => setView({ kind: "edit", address })} />}
        {view.kind === "new" && <NewAgent />}
        {view.kind === "edit" && <EditAgent key={view.address} address={view.address} onDone={() => setView({ kind: "staff" })} />}
      </div>
    </div>
  );
}
