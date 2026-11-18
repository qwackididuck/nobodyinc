"use client";

import { useState, useSyncExternalStore } from "react";

import { LiveProvider, useLive } from "@/lib/live";
import { Lobby } from "./projects/Lobby";
import { LeaveGuard, ProjectWindow } from "./projects/ProjectWindow";
import { unreadInBin } from "./chat/agents";
import { BIN_KEY } from "./chat/AvatarRail";
import { ChatPanel } from "./chat/ChatPanel";
import { useReadMarks } from "./chat/readMarks";
import { Kanban } from "./kanban/Kanban";
import { Office } from "./office/Office";
import { ScheduleNav, type ScheduleFocus } from "./schedules/nav";
import { Schedules } from "./schedules/Schedules";
import { StaffScreen } from "./staff/StaffScreen";
import { Settings } from "./settings/Settings";
import styles from "./shell/shell.module.css";
import { type Tab, TopBar } from "./shell/TopBar";

// What the chat panel shows: an agent's thread (by address), or the
// wastebasket of messages between agents.
// The office clock's key in the read marks.
const CLOCK_KEY = "~clock";

export type ChatTarget = { kind: "agent"; address: string } | { kind: "bin" };

// App shows what the window's URL says (ADR-0014): the projects page, or one
// project (?project=<id>), held by this window, and its control room: the
// top bar with its tabs, the chat panel on the left, and the selected tab
// (the office by default) in the main area.
export function App() {
  // The URL is only known in the browser (the page is prerendered).
  const search = useSyncExternalStore(neverChanges, () => window.location.search, () => null);
  if (search === null) return null;

  const params = new URLSearchParams(search);
  const project = params.get("project");
  if (!project) return <Lobby />;

  return (
    <ProjectWindow project={project} takeover={params.get("takeover") === "1"}>
      <LiveProvider>
        <ControlRoom />
      </LiveProvider>
    </ProjectWindow>
  );
}

// The window's URL changes only by loading another page.
function neverChanges(): () => void {
  return () => {};
}

function ControlRoom() {
  const { settings, messages } = useLive();
  const [tab, setTab] = useState<Tab>("office");
  const [target, setTarget] = useState<ChatTarget | null>(null);
  // What the Schedules tab shows first when opened from elsewhere; the tab
  // is keyed by it, so each new focus starts over.
  const [focus, setFocus] = useState<{ seq: number; value?: ScheduleFocus }>({ seq: 0 });
  const { marks, markSeen } = useReadMarks(settings?.project_id);
  const openChat = (address: string) => setTarget({ kind: "agent", address });
  const openSchedules = (value?: ScheduleFocus) => {
    setFocus((f) => ({ seq: f.seq + 1, value }));
    setTab("schedules");
  };

  return (
    <ScheduleNav value={openSchedules}>
    <div className={styles.app}>
      <LeaveGuard />
      <TopBar tab={tab} onTab={setTab} />
      <ChatPanel target={target} onTarget={setTarget} marks={marks} markSeen={markSeen} />
      <main className={styles.main}>
        {tab === "office" && (
          <Office
            selected={target?.kind === "agent" ? target.address : null}
            binUnread={unreadInBin(messages, marks[BIN_KEY])}
            onSelectAgent={openChat}
            onOpenBin={() => setTarget({ kind: "bin" })}
            clockSeen={marks[CLOCK_KEY]}
            onClockSeen={(at) => markSeen(CLOCK_KEY, at)}
          />
        )}
        {tab === "kanban" && <Kanban onOpenChat={openChat} />}
        {tab === "schedules" && <Schedules key={focus.seq} focus={focus.value} onOpenChat={openChat} />}
        {tab === "staff" && <StaffScreen onOpenChat={openChat} />}
        {tab === "settings" && <Settings />}
      </main>
    </div>
    </ScheduleNav>
  );
}
