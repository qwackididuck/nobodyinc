"use client";

import { useState } from "react";

import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import { Catalog } from "./Catalog";
import { EditAgent, NewAgent } from "./Editors";
import styles from "./staff.module.css";
import { Staff } from "./Staff";
import { pendingSubagents } from "./SubagentImport";

// The catalog and the new agent form may come with a team to recruit into,
// when opened from a team's "Add a member".
type View = { kind: "catalog"; team?: string } | { kind: "staff" } | { kind: "new"; team?: string } | { kind: "edit"; address: string };

interface Props {
  onOpenChat: (address: string) => void;
  // onImportSubagents opens the window importing the project's subagents.
  onImportSubagents: () => void;
}

const TABS: { kind: "catalog" | "staff" | "new"; label: string }[] = [
  { kind: "staff", label: "Staff" },
  { kind: "catalog", label: "Catalog" },
  { kind: "new", label: "New agent" },
];

// StaffScreen is the Staff tab: the staff of the office, team by team (teams,
// their flows and their members, editable), the catalog of templates as a
// character select screen to recruit from, and an editor for an agent's file
// (docs/agents.md). Recruits walk into the office; changes apply once an
// agent ends its turn, never in the middle of one.
export function StaffScreen({ onOpenChat, onImportSubagents }: Props) {
  const [view, setView] = useState<View>({ kind: "staff" });
  const { subagents } = useLive();
  const pending = pendingSubagents(subagents);

  const heading = {
    catalog: [view.kind === "catalog" && view.team ? `Choose a recruit for ${view.team}` : "Choose your recruit", "Templates from Nobody Inc and from ~/.nobody/catalog. A recruit is a copy: later changes to the template never touch it."],
    staff: ["Your staff", "Everyone in the office, team by team, and whom each team may start conversations with. Changes apply once the agent is idle, unless you ask for now."],
    new: [view.kind === "new" && view.team ? `New agent for ${view.team}` : "New agent", "Writes the agent's file in .nobody/ and starts it, or saves it as a template."],
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
        <div className={styles.subtabs} role="tablist" aria-label="Staff">
          {TABS.map((t) => (
            <button key={t.kind} type="button" role="tab" aria-selected={view.kind === t.kind} onClick={() => setView({ kind: t.kind })}>
              {t.label}
            </button>
          ))}
        </div>
      </header>
      <div className={styles.body}>
        {pending.length > 0 && (
          <div className={styles.importNotice}>
            <span>
              <strong>
                {pending.length} Claude Code subagent{pending.length > 1 ? "s" : ""}
              </strong>{" "}
              found in .claude/agents, not imported yet: make them agents of your teams, or dismiss them.
            </span>
            <button type="button" className={ui.btn} onClick={onImportSubagents}>
              Review
            </button>
          </div>
        )}
        {view.kind === "catalog" && <Catalog team={view.team} />}
        {view.kind === "staff" && (
          <Staff
            onOpenChat={onOpenChat}
            onEdit={(address) => setView({ kind: "edit", address })}
            onAddMember={(team, from) => setView({ kind: from === "catalog" ? "catalog" : "new", team })}
          />
        )}
        {view.kind === "new" && <NewAgent key={view.team} team={view.team} />}
        {view.kind === "edit" && <EditAgent key={view.address} address={view.address} onDone={() => setView({ kind: "staff" })} />}
      </div>
    </div>
  );
}
