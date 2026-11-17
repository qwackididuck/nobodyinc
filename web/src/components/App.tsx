"use client";

import { useState } from "react";

import { LiveProvider, useLive } from "@/lib/live";
import { unreadInBin } from "./chat/agents";
import { BIN_KEY } from "./chat/AvatarRail";
import { ChatPanel } from "./chat/ChatPanel";
import { useReadMarks } from "./chat/readMarks";
import { Kanban } from "./kanban/Kanban";
import { Office } from "./office/Office";
import { StaffScreen } from "./staff/StaffScreen";
import { Settings } from "./settings/Settings";
import styles from "./shell/shell.module.css";
import { type Tab, TopBar } from "./shell/TopBar";

// What the chat panel shows: an agent's thread (by address), or the
// wastebasket of messages between agents.
export type ChatTarget = { kind: "agent"; address: string } | { kind: "bin" };

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
  const openChat = (address: string) => setTarget({ kind: "agent", address });

  return (
    <div className={styles.app}>
      <TopBar tab={tab} onTab={setTab} />
      <ChatPanel target={target} onTarget={setTarget} marks={marks} markSeen={markSeen} />
      <main className={styles.main}>
        {tab === "office" && (
          <Office
            selected={target?.kind === "agent" ? target.address : null}
            binUnread={unreadInBin(messages, marks[BIN_KEY])}
            onSelectAgent={openChat}
            onOpenBin={() => setTarget({ kind: "bin" })}
          />
        )}
        {tab === "kanban" && <Kanban onOpenChat={openChat} />}
        {tab === "staff" && <StaffScreen onOpenChat={openChat} />}
        {tab === "settings" && <Settings />}
      </main>
    </div>
  );
}
