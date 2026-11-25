"use client";

import { type MouseEvent, useEffect, useRef, useState } from "react";

import { listProjects, type Project } from "@/lib/launcher";
import { checkNow, useUpdate } from "@/lib/update";
import { projectURL } from "@/lib/window";
import ui from "../ui/ui.module.css";
import { DeleteProject } from "./DeleteProject";
import { FolderBrowser } from "./FolderBrowser";
import { projectName, useProjectWindow } from "./ProjectWindow";
import styles from "./projects.module.css";

interface Props {
  // The project's folder, shown as a tooltip.
  path?: string;
}

// ProjectMenu is the project's name in the top bar, and the menu to switch
// project (ADR-0014): picking another project closes this one and opens the
// other in this window (Cmd/Ctrl-click: in a new window), and the window's
// project can be deleted from here.
export function ProjectMenu({ path }: Props) {
  const { project, hold, go } = useProjectWindow();
  const [open, setOpen] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState<"open" | "create" | null>(null);
  const [deleting, setDeleting] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const updates = useUpdate();

  useEffect(() => {
    if (!open) return;
    listProjects()
      .then((list) => {
        setProjects(list);
        setError(null);
      })
      .catch((e: Error) => setError(e.message));

    const close = (e: globalThis.MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", close);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const pick = (e: MouseEvent, url: string) => {
    setOpen(false);
    go(url, e.metaKey || e.ctrlKey);
  };

  const current = projects.find((p) => p.id === project);

  return (
    <div className={styles.switcher} ref={root}>
      <button
        type="button"
        className={styles.switchBtn}
        aria-haspopup="menu"
        aria-expanded={open}
        title={path}
        onClick={() => setOpen((o) => !o)}
      >
        <span className={styles.lamp} data-state={hold.status === "running" ? "running" : "starting"} />
        <code>{current?.name ?? projectName(project)}</code>
        <span className={styles.caret}>▾</span>
      </button>

      {open && (
        <div className={`${styles.menu} ${styles.switchMenu}`} role="menu">
          <div className={styles.menuHead}>SWITCH PROJECT · THIS ONE CLOSES</div>
          {error && <p className={ui.error}>{error}</p>}
          {projects.map((p) => (
            <button
              key={p.id}
              type="button"
              role="menuitem"
              className={styles.item}
              aria-current={p.id === project}
              disabled={p.missing}
              onClick={(e) => p.id !== project && pick(e, projectURL(p.id))}
            >
              <span className={styles.lamp} data-state={p.id === project ? "running" : p.held ? "starting" : "stopped"} />
              <span className={styles.itemName}>
                <code>{p.name}</code>
                <small>{p.path}</small>
              </span>
              <span className={styles.tag}>{p.id === project ? "THIS WINDOW" : p.missing ? "MISSING" : p.held ? "OTHER WINDOW" : ""}</span>
            </button>
          ))}
          <div className={styles.sep} />
          <button type="button" role="menuitem" className={`${styles.item} ${styles.cmd}`} onClick={(e) => pick(e, projectURL(null))}>
            All projects…
          </button>
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd}`}
            onClick={() => {
              setOpen(false);
              setBrowsing("open");
            }}
          >
            Open folder…
          </button>
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd}`}
            onClick={() => {
              setOpen(false);
              setBrowsing("create");
            }}
          >
            + New project
          </button>
          <div className={styles.sep} />
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd}`}
            disabled={updates.checking}
            onClick={() => {
              setOpen(false);
              void checkNow();
            }}
          >
            Check for updates…
          </button>
          <div className={styles.sep} />
          <button
            type="button"
            role="menuitem"
            className={`${styles.item} ${styles.cmd} ${styles.cmdDanger}`}
            onClick={() => {
              setOpen(false);
              setDeleting(true);
            }}
          >
            Delete this project…
          </button>
        </div>
      )}

      {browsing && (
        <FolderBrowser mode={browsing} onClose={() => setBrowsing(null)} onOpened={(p) => go(projectURL(p.id, p.created))} />
      )}
      {deleting && (
        <DeleteProject
          project={current ?? { id: project, name: projectName(project) }}
          onClose={() => setDeleting(false)}
          onDeleted={() => window.location.assign(projectURL(null))}
        />
      )}
    </div>
  );
}
