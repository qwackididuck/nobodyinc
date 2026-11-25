"use client";

import { useEffect, useState, useSyncExternalStore } from "react";

import { LiveProvider, useLive } from "@/lib/live";
import { Lobby } from "./projects/Lobby";
import { LeaveGuard, ProjectWindow } from "./projects/ProjectWindow";
import { unreadInBin } from "./chat/agents";
import { BIN_KEY } from "./chat/AvatarRail";
import { ChatPanel } from "./chat/ChatPanel";
import { useReadMarks } from "./chat/readMarks";
import { useChatWidth } from "./chat/useChatWidth";
import { Kanban } from "./kanban/Kanban";
import { Office } from "./office/Office";
import { ScheduleNav, type ScheduleFocus } from "./schedules/nav";
import { Schedules } from "./schedules/Schedules";
import { StaffScreen } from "./staff/StaffScreen";
import { pendingSubagents, SubagentImport } from "./staff/SubagentImport";
import { Settings } from "./settings/Settings";
import { ShellDrawer } from "./shell/ShellDrawer";
import styles from "./shell/shell.module.css";
import { type Tab, TopBar } from "./shell/TopBar";
import { UpdateNotice } from "./shell/UpdateNotice";

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
  if (!project) {
    return (
      <>
        <Lobby />
        <UpdateNotice />
      </>
    );
  }

  return (
    <ProjectWindow project={project} takeover={params.get("takeover") === "1"}>
      <LiveProvider>
        <ControlRoom setup={params.get("setup") === "1"} />
      </LiveProvider>
      <UpdateNotice />
    </ProjectWindow>
  );
}

// The window's URL changes only by loading another page.
function neverChanges(): () => void {
  return () => {};
}

// ControlRoom is one project's control room. setup is true on the first
// opening of a project the launcher has just set up: the window importing
// its subagents then opens by itself, if it has any (ADR-0018).
function ControlRoom({ setup }: { setup: boolean }) {
  const { settings, messages, subagents, loaded } = useLive();
  const [tab, setTab] = useState<Tab>("office");
  const [importing, setImporting] = useState(false);
  // The terminal drawer is shown or hidden from any tab, by the top bar's
  // button or Ctrl+` (captured, so that a focused terminal does not swallow it).
  const [terminalOpen, setTerminalOpen] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && !e.metaKey && !e.altKey && e.key === "`") {
        e.preventDefault();
        setTerminalOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);
  // Closing the window ends the first opening's offer: afterwards, the STAFF
  // badge and the Staff screen's notice lead to it.
  const [setupDone, setSetupDone] = useState(false);
  const showImport = importing || (setup && !setupDone && loaded.subagents === true && pendingSubagents(subagents).length > 0);

  // setup=1 is read once: a reload must not offer the import again.
  useEffect(() => {
    if (!setup) return;
    const url = new URL(window.location.href);
    url.searchParams.delete("setup");
    window.history.replaceState(null, "", url);
  }, [setup]);
  const [target, setTarget] = useState<ChatTarget | null>(null);
  // What the Schedules tab shows first when opened from elsewhere; the tab
  // is keyed by it, so each new focus starts over.
  const [focus, setFocus] = useState<{ seq: number; value?: ScheduleFocus }>({ seq: 0 });
  const { marks, markSeen } = useReadMarks(settings?.project_id);
  const chatWidth = useChatWidth();
  const openChat = (address: string) => setTarget({ kind: "agent", address });
  const openSchedules = (value?: ScheduleFocus) => {
    setFocus((f) => ({ seq: f.seq + 1, value }));
    setTab("schedules");
  };

  return (
    <ScheduleNav value={openSchedules}>
    <div className={styles.app} style={{ "--chat-w": `${chatWidth.width}px` } as React.CSSProperties}>
      <LeaveGuard />
      <TopBar tab={tab} onTab={setTab} terminalOpen={terminalOpen} onToggleTerminal={() => setTerminalOpen((open) => !open)} />
      <ChatPanel target={target} onTarget={setTarget} marks={marks} markSeen={markSeen} width={chatWidth} />
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
        {tab === "staff" && <StaffScreen onOpenChat={openChat} onImportSubagents={() => setImporting(true)} />}
        {tab === "settings" && <Settings />}
      </main>
      <ShellDrawer open={terminalOpen} onOpenChange={setTerminalOpen} />
      {showImport && (
        <SubagentImport
          onClose={() => {
            setImporting(false);
            setSetupDone(true);
          }}
        />
      )}
    </div>
    </ScheduleNav>
  );
}
