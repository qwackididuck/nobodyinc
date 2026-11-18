"use client";

import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

import { type Agent, listAgents, setProject } from "@/lib/api";
import { useLive } from "@/lib/live";
import { type Hold, projectURL, useHold } from "@/lib/window";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import { BareTopBar } from "./BareTopBar";
import styles from "./projects.module.css";

// What the window's components (the top bar's project menu) need to know
// about the window's project.
interface ProjectWindowContext {
  project: string;
  hold: Hold;
  // go shows another page in this window (closing this project), or in a
  // new window. Agents at work are a reason to ask first.
  go: (url: string, newWindow?: boolean) => void;
}

const Context = createContext<ProjectWindowContext | null>(null);

export function useProjectWindow(): ProjectWindowContext {
  const value = useContext(Context);
  if (!value) throw new Error("useProjectWindow must be used inside ProjectWindow");
  return value;
}

interface Props {
  project: string;
  // takeover: the window was opened to take the project from another one
  // (the projects page's "Open here").
  takeover: boolean;
  // children is the control room, shown once the project's daemon runs.
  children: ReactNode;
}

// ProjectWindow holds the window's project (ADR-0014) and shows, in place of
// the control room, how that goes: opening, open in another window, taken
// over. Once the daemon has run, the control room stays, with a band over it
// when the daemon stopped on its own or the launcher is out of reach.
export function ProjectWindow({ project, takeover, children }: Props) {
  // Before anything renders: every API call goes to this project.
  useState(() => setProject(project));

  const hold = useHold(project, takeover);
  const [shown, setShown] = useState(false);
  const [leaving, setLeaving] = useState<{ url: string; working: Agent[] } | null>(null);

  if (hold.status === "running" && !shown) setShown(true);

  // "Open here" is a one-time request: a reload must not take the project
  // over again.
  useEffect(() => {
    if (takeover) window.history.replaceState(null, "", projectURL(project));
  }, [takeover, project]);

  async function go(url: string, newWindow = false) {
    if (newWindow) {
      window.open(url, "_blank");
      return;
    }
    const working = hold.status === "running" ? (await listAgents().catch(() => [])).filter((a) => a.status === "working") : [];
    if (working.length > 0) {
      setLeaving({ url, working });
      return;
    }
    await leave(url);
  }

  async function leave(url: string) {
    await hold.release();
    window.location.assign(url);
  }

  const context = { project, hold, go: (url: string, newWindow?: boolean) => void go(url, newWindow) };

  return (
    <Context.Provider value={context}>
      {shown && (hold.status === "running" || hold.status === "crashed" || hold.status === "reconnecting" || hold.status === "opening") ? (
        <>
          {children}
          <StatusBand hold={hold} />
        </>
      ) : (
        <div className={styles.page}>
          <BareTopBar />
          <div className={styles.center}>
            <WindowState hold={hold} project={project} onBack={() => window.location.assign(projectURL(null))} />
          </div>
        </div>
      )}
      {leaving && (
        <Modal label="Close the project" onClose={() => setLeaving(null)}>
          <h3 className={ui.title}>Close this project?</h3>
          <p className={ui.sub}>
            <b>
              {leaving.working.length} agent{leaving.working.length > 1 ? "s are" : " is"} working
            </b>{" "}
            ({leaving.working.map((a) => a.address).join(", ")}). Closing the project stops them now: their current turn is lost. Mail, tickets and the
            feature are kept for next time.
          </p>
          <div className={styles.actions}>
            <button type="button" className={ui.btn} onClick={() => setLeaving(null)}>
              Stay here
            </button>
            <button type="button" className={ui.btnPrimary} onClick={() => void leave(leaving.url)}>
              Close it
            </button>
          </div>
        </Modal>
      )}
    </Context.Provider>
  );
}

// WindowState is what the window shows instead of the control room.
function WindowState({ hold, project, onBack }: { hold: Hold; project: string; onBack: () => void }) {
  switch (hold.status) {
    case "held":
      return (
        <div className={styles.stateCard} data-tone="warn">
          <h2>This project is open in another window</h2>
          <p>A project is shown in one window at a time. Opening it here closes it in the other window; its agents keep working.</p>
          <div className={styles.actions}>
            <button type="button" className={ui.btn} onClick={onBack}>
              Back to projects
            </button>
            <button type="button" className={ui.btnPrimary} onClick={hold.takeOver}>
              Open here
            </button>
          </div>
        </div>
      );
    case "taken":
      return (
        <div className={styles.stateCard} data-tone="warn">
          <h2>Opened in another window</h2>
          <p>The project now lives in another window. Its agents keep working there.</p>
          <div className={styles.actions}>
            <button type="button" className={ui.btn} onClick={onBack}>
              Back to projects
            </button>
            <button type="button" className={ui.btnPrimary} onClick={hold.takeOver}>
              Take it back
            </button>
          </div>
        </div>
      );
    case "crashed":
      return (
        <div className={styles.stateCard} data-tone="warn">
          <h2>The project did not start</h2>
          <p>
            <code>{hold.error}</code>
          </p>
          <div className={styles.actions}>
            <button type="button" className={ui.btn} onClick={onBack}>
              Back to projects
            </button>
            <button type="button" className={ui.btnPrimary} onClick={hold.restart}>
              Try again
            </button>
          </div>
        </div>
      );
    default:
      return (
        <div className={styles.stateCard}>
          <div className={styles.elevator} aria-hidden="true">
            <i />
            <i />
          </div>
          <h2>Opening {projectName(project)}</h2>
          <p>{hold.status === "reconnecting" ? "Reaching the launcher…" : "The agents are arriving at their desks…"}</p>
        </div>
      );
  }
}

// StatusBand covers the top of the control room when the project's daemon
// stopped on its own, is starting again, or the launcher is out of reach.
function StatusBand({ hold }: { hold: Hold }) {
  if (hold.status === "crashed") {
    return (
      <div className={styles.banner} role="alert">
        <b>THE PROJECT STOPPED</b>
        <code title={hold.error}>{hold.error}</code>
        <button type="button" className={ui.btn} onClick={hold.restart}>
          Restart
        </button>
      </div>
    );
  }
  if (hold.status === "reconnecting" || hold.status === "opening") {
    return (
      <div className={styles.banner} data-tone="warn" role="status">
        <b>{hold.status === "reconnecting" ? "RECONNECTING TO THE LAUNCHER…" : "THE PROJECT IS STARTING…"}</b>
        <code />
      </div>
    );
  }
  return null;
}

// LeaveGuard asks the browser to confirm closing the tab while agents work:
// closing the window stops the project (after the grace period). Browsers
// only show their own generic prompt.
export function LeaveGuard() {
  const { agents } = useLive();
  const working = agents.some((a) => a.status === "working");

  useEffect(() => {
    if (!working) return;
    const onLeave = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onLeave);
    return () => window.removeEventListener("beforeunload", onLeave);
  }, [working]);

  return null;
}

// projectName is the folder name in a project id ("<folder>-<hash>").
export function projectName(id: string): string {
  return id.replace(/-[0-9a-f]{8}$/, "");
}
