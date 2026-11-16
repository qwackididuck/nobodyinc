"use client";

import { useState } from "react";

import { LiveProvider, useLive } from "@/lib/live";
import { unreadInBin } from "./chat/agents";
import { BIN_KEY } from "./chat/AvatarRail";
import { ChatPanel } from "./chat/ChatPanel";
import { useReadMarks } from "./chat/readMarks";
import { Office } from "./office/Office";
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
  const { settings, messages } = useLive();
  const [tab, setTab] = useState<Tab>("office");
  const [target, setTarget] = useState<ChatTarget | null>(null);
  const { marks, markSeen } = useReadMarks(settings?.project_id);

  return (
    <div className={styles.app}>
      <TopBar tab={tab} onTab={setTab} />
      <ChatPanel target={target} onTarget={setTarget} marks={marks} markSeen={markSeen} />
      <main className={styles.main}>
        {tab === "office" ? (
          <Office
            selected={target?.kind === "agent" ? target.address : null}
            binUnread={unreadInBin(messages, marks[BIN_KEY])}
            onSelectAgent={(address) => setTarget({ kind: "agent", address })}
            onOpenBin={() => setTarget({ kind: "bin" })}
            onHireDirector={() => setTarget({ kind: "hire-director" })}
          />
        ) : (
          <ComingSoon tab={tab} />
        )}
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
