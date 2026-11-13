"use client";

import { useState } from "react";

import { type Message, listAgents, listApprovals, listMessages } from "@/lib/api";
import { usePolling } from "@/lib/usePolling";
import { AgentList } from "./AgentList";
import { ApprovalQueue } from "./ApprovalQueue";
import { Composer } from "./Composer";
import styles from "./ControlRoom.module.css";
import { MessageFeed } from "./MessageFeed";
import { RecruitmentView } from "./RecruitmentView";
import { TerminalView } from "./TerminalView";
import { TicketsView } from "./TicketsView";

const REFRESH_MS = 2000;

const VIEW_LABELS = { terminal: "Terminal", tickets: "Tickets", recruitment: "Recruitment" } as const;
// Faster: an agent is blocked while its permission request waits.
const APPROVALS_REFRESH_MS = 1000;

// ControlRoom is the supervision screen: teams on the left, the selected
// agent's terminal in the middle, pending approvals and the mail on the right.
export function ControlRoom() {
  const agents = usePolling(listAgents, REFRESH_MS);
  const messages = usePolling(listMessages, REFRESH_MS);
  const approvals = usePolling(listApprovals, APPROVALS_REFRESH_MS);
  const [selected, setSelected] = useState<string | null>(null);
  const [focusMail, setFocusMail] = useState(false);
  const [replyTo, setReplyTo] = useState<Message | null>(null);
  const [view, setView] = useState<"terminal" | "tickets" | "recruitment">("terminal");

  const agentList = agents.data ?? [];
  const current = agentList.find((a) => a.address === selected) ?? null;
  const running = current && current.status !== "stopped" && current.status !== "exited";
  const teams = [...new Set(agentList.map((a) => a.team))];

  return (
    <main className={styles.room}>
      <AgentList agents={agentList} selected={selected} onSelect={setSelected} onChange={agents.refresh} />

      <section className={styles.panel} aria-label={VIEW_LABELS[view]}>
        <div className={styles.tabs}>
          <button type="button" className={view === "terminal" ? styles.tabActive : styles.tab} onClick={() => setView("terminal")}>
            {current ? `Terminal — ${current.address}` : "Terminal"}
          </button>
          <button type="button" className={view === "tickets" ? styles.tabActive : styles.tab} onClick={() => setView("tickets")}>
            Tickets
          </button>
          <button type="button" className={view === "recruitment" ? styles.tabActive : styles.tab} onClick={() => setView("recruitment")}>
            Recruitment
          </button>
        </div>
        {agents.error && <p className={styles.error}>Daemon unreachable: {agents.error}</p>}
        {view === "tickets" && <TicketsView teams={teams} />}
        {view === "recruitment" && <RecruitmentView selected={current} onChanged={() => void agents.refresh()} />}
        {view === "terminal" && !current && <p className={styles.empty}>Select an agent to see its terminal.</p>}
        {view === "terminal" && current && !running && <p className={styles.empty}>This agent is not running.</p>}
        {view === "terminal" && current && running && <TerminalView key={current.address} address={current.address} />}
      </section>

      <section className={styles.panel} aria-label="Mail">
        <ApprovalQueue
          approvals={approvals.data ?? []}
          onDecided={() => {
            void approvals.refresh();
            void agents.refresh();
          }}
        />
        <h2 className={styles.panelTitle}>
          Mail
          {selected && (
            <label className={styles.checkbox}>
              <input type="checkbox" checked={focusMail} onChange={(e) => setFocusMail(e.target.checked)} /> only{" "}
              {selected}
            </label>
          )}
        </h2>
        <MessageFeed
          messages={messages.data ?? []}
          focus={focusMail ? selected : null}
          onReply={setReplyTo}
        />
        <Composer
          agents={agentList}
          defaultTo={selected}
          replyTo={replyTo}
          onCancelReply={() => setReplyTo(null)}
          onSent={messages.refresh}
        />
      </section>
    </main>
  );
}
