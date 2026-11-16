"use client";

import { useLive } from "@/lib/live";
import styles from "./shell.module.css";

export const TABS = [
  { id: "office", label: "Office" },
  { id: "kanban", label: "Kanban" },
  { id: "recruitment", label: "Recruitment" },
  { id: "settings", label: "Settings" },
] as const;

export type Tab = (typeof TABS)[number]["id"];

interface Props {
  tab: Tab;
  onTab: (tab: Tab) => void;
}

// TopBar shows the project, the tabs and whether the live connection to the
// daemon is up.
export function TopBar({ tab, onTab }: Props) {
  const { settings, connected } = useLive();
  const project = settings ? (settings.project_dir.split("/").filter(Boolean).pop() ?? settings.project_dir) : "…";

  return (
    <header className={styles.topbar}>
      <div className={styles.brand}>
        <BrandMark />
        Nobody Inc
      </div>
      <span className={styles.project} title={settings?.project_dir}>
        Project <code>{project}</code>
      </span>
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
          </button>
        ))}
      </nav>
      <span className={styles.daemon} data-connected={connected} title={connected ? "Live updates on" : "Reconnecting to the daemon…"}>
        {connected ? (settings?.address ?? "Daemon") : "Reconnecting…"}
      </span>
    </header>
  );
}

// BrandMark is a dotted, pixel outline of a person: nobody.
function BrandMark() {
  return (
    <svg className={styles.brandMark} viewBox="0 0 13 13" shapeRendering="crispEdges" aria-hidden="true">
      <path
        fill="var(--accent)"
        d="M4 1h1v1H4zM6 1h1v1H6zM8 1h1v1H8zM3 3h1v1H3zM9 3h1v1H9zM4 5h1v1H4zM6 5h1v1H6zM8 5h1v1H8zM2 9h1v1H2zM4 8h1v1H4zM6 8h1v1H6zM8 8h1v1H8zM10 9h1v1h-1zM1 11h1v1H1zM11 11h1v1h-1z"
      />
    </svg>
  );
}
