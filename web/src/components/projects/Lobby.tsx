"use client";

import { type MouseEvent, useCallback, useEffect, useState } from "react";

import { deleteProject, listProjects, type Project, type ProjectActivity, projectActivity } from "@/lib/launcher";
import { projectURL, windowID } from "@/lib/window";
import ui from "../ui/ui.module.css";
import { BareTopBar } from "./BareTopBar";
import { DeleteProject } from "./DeleteProject";
import { FolderBrowser } from "./FolderBrowser";
import styles from "./projects.module.css";

// How often the page reads the projects again, and the activity of those
// open in other windows.
const REFRESH_MS = 10_000;

// Lobby is the projects page (ADR-0014): what a window shows before it opens
// a project. Each project is a building, lit when it is open in a window,
// with what its agents are doing. Opening a project shows it in this window
// (Cmd/Ctrl-click: in a new one).
export function Lobby() {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [activity, setActivity] = useState<Record<string, ProjectActivity>>({});
  const [error, setError] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState<"open" | "create" | null>(null);
  const [deleting, setDeleting] = useState<Project | null>(null);

  const refresh = useCallback(async () => {
    try {
      const list = await listProjects();
      setProjects(list);
      setError(null);

      const running = list.filter((p) => p.held && p.state === "running");
      const entries = await Promise.all(
        running.map(async (p) => [p.id, await projectActivity(p.id).catch(() => undefined)] as const),
      );
      setActivity(Object.fromEntries(entries.filter((e): e is readonly [string, ProjectActivity] => e[1] !== undefined)));
    } catch (e) {
      setError(`The launcher cannot be reached: ${(e as Error).message}`);
    }
  }, []);

  useEffect(() => {
    // The first load, then a refresh every REFRESH_MS and when the window
    // comes back to the front.
    const first = setTimeout(() => void refresh(), 0);
    const timer = setInterval(() => void refresh(), REFRESH_MS);
    const onFocus = () => void refresh();
    window.addEventListener("focus", onFocus);
    return () => {
      clearTimeout(first);
      clearInterval(timer);
      window.removeEventListener("focus", onFocus);
    };
  }, [refresh]);

  const open = (e: MouseEvent, url: string) => {
    if (e.metaKey || e.ctrlKey) window.open(url, "_blank");
    else window.location.assign(url);
  };

  return (
    <div className={styles.page}>
      <BareTopBar lobby />
      <div className={styles.lobby}>
        <div className={styles.lobbyHead}>
          <div className={styles.grow}>
            <h1 className={ui.title}>Projects</h1>
            <p className={ui.sub}>One window, one project: a project runs while its window is open, and stops when you close it.</p>
          </div>
          <button type="button" className={ui.btn} onClick={() => setBrowsing("open")}>
            Open folder…
          </button>
          <button type="button" className={ui.btnPrimary} onClick={() => setBrowsing("create")}>
            + New project
          </button>
        </div>

        {error && <p className={ui.error}>{error}</p>}

        <div className={styles.grid}>
          {projects?.length === 0 && (
            <div className={styles.empty}>
              <b>No project yet</b>
              <span>Open a folder of yours, or create a new project.</span>
            </div>
          )}
          {projects?.map((p) => (
            <ProjectCard
              key={p.id}
              project={p}
              activity={activity[p.id]}
              onOpen={(e) => open(e, p.held ? `${projectURL(p.id)}&takeover=1` : projectURL(p.id))}
              onDelete={() => setDeleting(p)}
              onRemove={() => void deleteProject(p.id, windowID()).then(refresh, (err: Error) => setError(err.message))}
            />
          ))}
        </div>
      </div>

      {browsing && (
        <FolderBrowser mode={browsing} onClose={() => setBrowsing(null)} onOpened={(p) => window.location.assign(projectURL(p.id))} />
      )}
      {deleting && (
        <DeleteProject
          project={deleting}
          onClose={() => setDeleting(null)}
          onDeleted={() => {
            setDeleting(null);
            void refresh();
          }}
        />
      )}
    </div>
  );
}

interface CardProps {
  project: Project;
  activity?: ProjectActivity;
  onOpen: (e: MouseEvent) => void;
  onDelete: () => void;
  // onRemove forgets a project whose folder disappeared.
  onRemove: () => void;
}

// ProjectCard is a project as a building.
function ProjectCard({ project: p, activity, onOpen, onDelete, onRemove }: CardProps) {
  const [menu, setMenu] = useState(false);
  const open = p.held;

  return (
    <article className={styles.card} data-missing={p.missing}>
      <Facade lit={open ? 3 + Math.min(activity?.working ?? 0, 5) : 0} sign={open} door={!p.missing} />
      <div className={styles.body}>
        <h3>
          <span className={styles.lamp} data-state={open ? p.state : "stopped"} />
          {p.name}
        </h3>
        <div className={styles.path} title={p.path}>
          {p.path}
        </div>
        <div className={styles.chips}>
          {p.missing && <span className={`${ui.chip} ${styles.chipDanger}`}>Folder missing</span>}
          {open && <span className={`${ui.chip} ${styles.chipOk}`}>Open in another window</span>}
          {!p.missing && p.git && <span className={ui.chip}>git</span>}
          {!p.missing && !p.git && <span className={`${ui.chip} ${styles.chipWarn}`}>Not a git repository</span>}
          {!p.missing && !p.trusted && <span className={`${ui.chip} ${styles.chipWarn}`}>Not trusted by Claude Code</span>}
        </div>
        <div className={styles.stats}>
          {p.missing ? (
            <span>Moved or deleted outside Nobody Inc</span>
          ) : open && activity ? (
            <>
              <span>
                <b>{activity.working}</b> agent{activity.working === 1 ? "" : "s"} working
              </span>
              <span>
                <b>${activity.monthUSD.toFixed(2)}</b> this month
              </span>
              {activity.feature && (
                <span>
                  feature <b>{activity.feature}</b>
                </span>
              )}
            </>
          ) : (
            <span>{p.last_opened ? <>Last opened <b>{new Date(p.last_opened).toLocaleString()}</b></> : "Never opened"}</span>
          )}
        </div>
      </div>
      <div className={styles.foot}>
        <span className={styles.grow} />
        {p.missing ? (
          <button type="button" className={ui.btnDanger} onClick={onRemove}>
            Remove
          </button>
        ) : (
          <>
            <button type="button" className={styles.dots} aria-label={`More for ${p.name}`} aria-expanded={menu} onClick={() => setMenu((m) => !m)}>
              ⋯
            </button>
            <button type="button" className={open ? ui.btn : ui.btnPrimary} onClick={onOpen} title={open ? "Takes it from the other window" : undefined}>
              {open ? "Open here" : "Open"}
            </button>
          </>
        )}
      </div>
      {menu && (
        <div className={`${styles.menu} ${styles.cardMenu}`} role="menu" onMouseLeave={() => setMenu(false)}>
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd}`}
            onClick={() => {
              setMenu(false);
              void navigator.clipboard?.writeText(p.path);
            }}
          >
            Copy folder path
          </button>
          <div className={styles.sep} />
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd} ${styles.cmdDanger}`}
            onClick={() => {
              setMenu(false);
              onDelete();
            }}
          >
            Delete project…
          </button>
        </div>
      )}
    </article>
  );
}

// The lit windows' pattern: the first ones are lit first.
const WINDOW_ORDER = [0, 4, 1, 6, 3, 5, 2, 7];

// Facade is the building of a project card: lit windows when it is open.
function Facade({ lit, sign, door }: { lit: number; sign: boolean; door: boolean }) {
  const litSet = new Set(WINDOW_ORDER.slice(0, lit));
  return (
    <div className={styles.facade} aria-hidden="true">
      {sign && <span className={styles.sign}>OPEN</span>}
      <div className={styles.windows}>
        {Array.from({ length: 8 }, (_, i) => (
          <i key={i} data-lit={litSet.has(i) ? (i === 3 ? "soft" : "true") : "false"} />
        ))}
      </div>
      {door && <div className={styles.door} />}
    </div>
  );
}
