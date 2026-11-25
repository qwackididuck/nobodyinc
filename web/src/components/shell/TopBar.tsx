"use client";

import { useLive } from "@/lib/live";
import { AutomationBanner, AutomationButton } from "../automation/AutomationControls";
import { ProjectMenu } from "../projects/ProjectMenu";
import { attention } from "../schedules/scheduling";
import { pendingSubagents } from "../staff/SubagentImport";
import styles from "./shell.module.css";

export const TABS = [
  { id: "office", label: "Office" },
  { id: "kanban", label: "Kanban" },
  { id: "schedules", label: "Schedules" },
  { id: "staff", label: "Staff" },
  { id: "settings", label: "Settings" },
] as const;

export type Tab = (typeof TABS)[number]["id"];

interface Props {
  tab: Tab;
  onTab: (tab: Tab) => void;
  // The terminal drawer (ShellDrawer): whether it is shown, and the button's action.
  terminalOpen: boolean;
  onToggleTerminal: () => void;
}

// TopBar shows the project (and its menu, to switch project), the tabs and
// whether the live connection to the project's daemon is up, and the button
// of the terminal drawer.
export function TopBar({ tab, onTab, terminalOpen, onToggleTerminal }: Props) {
  const { settings, connected, schedules, scheduleProposals, subagents } = useLive();
  // Schedules in alert and the director's proposals wait for the human; so
  // do the project's subagents not imported nor dismissed yet (ADR-0018).
  const badges: Partial<Record<Tab, number>> = {
    schedules: attention(schedules, scheduleProposals),
    staff: pendingSubagents(subagents).length,
  };

  return (
    <div className={styles.topArea}>
      <header className={styles.topbar}>
        <div className={styles.brand}>
          <BrandMark />
          Nobody Inc
        </div>
        <ProjectMenu path={settings?.project_dir} />
        <nav className={styles.tabs} role="tablist" aria-label="Views">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              className={styles.tab}
              onClick={() => onTab(t.id)}
            >
              {t.label}
              {badges[t.id] ? (
                <span className={styles.tabBadge} title="Waiting for you">
                  {badges[t.id]}
                </span>
              ) : null}
            </button>
          ))}
        </nav>
        <span className={styles.daemon} data-connected={connected} title={connected ? "Live updates on" : "Reconnecting to the daemon…"}>
          {connected ? "Live" : "Reconnecting…"}
        </span>
        <AutomationButton />
        <button
          type="button"
          className={styles.terminalToggle}
          aria-pressed={terminalOpen}
          onClick={onToggleTerminal}
          title="A terminal in the project's folder (Ctrl+`)"
        >
          &gt;_ Terminal
        </button>
      </header>
      <AutomationBanner />
    </div>
  );
}

// BrandMark is a dotted, pixel outline of a person: nobody.
export function BrandMark() {
  return (
    <svg className={styles.brandMark} viewBox="0 0 13 13" shapeRendering="crispEdges" aria-hidden="true">
      <path
        fill="var(--accent)"
        d="M4 1h1v1H4zM6 1h1v1H6zM8 1h1v1H8zM3 3h1v1H3zM9 3h1v1H9zM4 5h1v1H4zM6 5h1v1H6zM8 5h1v1H8zM2 9h1v1H2zM4 8h1v1H4zM6 8h1v1H6zM8 8h1v1H8zM10 9h1v1h-1zM1 11h1v1H1zM11 11h1v1h-1z"
      />
    </svg>
  );
}
