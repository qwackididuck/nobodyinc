"use client";

import { useState } from "react";

import { LiveProvider } from "@/lib/live";
import { ChatPanel } from "./chat/ChatPanel";
import styles from "./shell/shell.module.css";
import { type Tab, TopBar } from "./shell/TopBar";

// What the chat panel shows: an agent's thread (by address), the
// wastebasket of messages between agents, or how to recruit the director
// while the project has none.
export type ChatTarget = { kind: "agent"; address: string } | { kind: "bin" } | { kind: "hire-director" };

// App is the control room: the top bar with its tabs, the chat panel on the
// left, and the selected tab (the office by default) in the main area.
export function App() {
  return (
    <LiveProvider>
      <ControlRoom />
    </LiveProvider>
  );
}

function ControlRoom() {
  const [tab, setTab] = useState<Tab>("office");
  const [target, setTarget] = useState<ChatTarget | null>(null);

  return (
    <div className={styles.app}>
      <TopBar tab={tab} onTab={setTab} />
      <ChatPanel target={target} onTarget={setTarget} />
      <main className={styles.main}>
        <ComingSoon tab={tab} />
      </main>
    </div>
  );
}

// ComingSoon holds the place of the tabs built in the next steps.
function ComingSoon({ tab }: { tab: Tab }) {
  const names: Record<Tab, string> = { office: "Office", kanban: "Kanban", recruitment: "Recruitment", settings: "Settings" };
  return (
    <div className={styles.placeholder}>
      <strong>{names[tab]}</strong>
      <span>This tab arrives in the next step of the redesign.</span>
    </div>
  );
}
