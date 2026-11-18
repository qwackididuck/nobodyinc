"use client";

import { type FormEvent, useEffect, useState } from "react";

import { type Directory, type DirectoryListing, listDirs, openProject, type Project } from "@/lib/launcher";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./projects.module.css";

interface Props {
  // open: choose an existing folder; create: name a new one in a folder.
  mode: "open" | "create";
  onClose: () => void;
  // onOpened gets the project, set up (and created) by the launcher.
  onOpened: (project: Project) => void;
}

// FolderBrowser chooses a project's folder on the machine, through the
// launcher (GET /api/fs/dirs): folders only, each saying whether it is a git
// repository, already has a .nobody/, or is a known project. Opening a
// folder without .nobody/ sets it up (the director, Claude Code's trust);
// creating one also makes it a git repository.
export function FolderBrowser({ mode, onClose, onOpened }: Props) {
  const [listing, setListing] = useState<DirectoryListing | null>(null);
  const [selected, setSelected] = useState<Directory | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function browse(path?: string) {
    setSelected(null);
    listDirs(path)
      .then((l) => {
        setListing(l);
        setError(null);
      })
      .catch((e: Error) => setError(e.message));
  }

  // The home directory first.
  useEffect(() => {
    listDirs()
      .then(setListing)
      .catch((e: Error) => setError(e.message));
  }, []);

  // The folder to open: the one selected, or the one shown, unless that is
  // the home directory or the root (never a project by a stray click).
  const shownCanOpen = listing !== null && listing.path !== listing.home && listing.parent !== undefined;
  const target =
    mode === "open"
      ? (selected?.path ?? (shownCanOpen ? listing.path : null))
      : listing && name.trim()
        ? `${listing.path}/${name.trim()}`
        : null;
  const taken = mode === "create" && listing?.dirs.some((d) => d.name === name.trim());

  async function submit(e: FormEvent) {
    e.preventDefault();
    e.stopPropagation();
    if (!target || taken) return;
    setBusy(true);
    setError(null);
    try {
      onOpened(await openProject(target, mode === "create"));
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <Modal label={mode === "open" ? "Open a folder" : "New project"} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)} style={{ display: "grid", gap: 12 }}>
        <h3 className={ui.title}>{mode === "open" ? "Open a folder" : "New project"}</h3>

        {mode === "create" && (
          <label className={ui.label}>
            NAME
            <input className={ui.input} value={name} autoFocus placeholder="my-service" onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        {mode === "create" && <span className={ui.label}>IN</span>}

        {listing && <Crumbs listing={listing} onBrowse={browse} />}

        <div className={styles.folders} data-small={mode === "create"} role="listbox" aria-label="Folders">
          {listing?.dirs.length === 0 && <p className={ui.sub} style={{ padding: 10 }}>No sub-folder here.</p>}
          {listing?.dirs.map((d) => (
            <button
              key={d.path}
              type="button"
              role="option"
              className={styles.folder}
              aria-selected={mode === "open" && selected?.path === d.path}
              title="Double-click to open the folder"
              onClick={() => (mode === "open" ? setSelected(d) : browse(d.path))}
              onDoubleClick={() => browse(d.path)}
            >
              <span className={styles.folderIcon} />
              <span className={styles.grow}>{d.name}</span>
              {d.git && <span className={ui.chip}>git</span>}
              {d.nobody && <span className={`${ui.chip} ${styles.chipAccent}`}>.nobody</span>}
              {d.project && <span className={`${ui.chip} ${styles.chipOk}`}>known</span>}
            </button>
          ))}
        </div>

        <Hint mode={mode} listing={listing} selected={selected} target={target} taken={Boolean(taken)} />
        {error && <p className={ui.error}>{error}</p>}

        <div className={styles.actions}>
          <button type="button" className={ui.btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={ui.btnPrimary} disabled={busy || !target || taken}>
            {mode === "create" ? "Create project" : `Open ${baseName(target ?? "")}`}
          </button>
        </div>
      </form>
    </Modal>
  );
}

// Crumbs is the folder shown, each part clickable, and the way up.
function Crumbs({ listing, onBrowse }: { listing: DirectoryListing; onBrowse: (path: string) => void }) {
  const inHome = listing.path === listing.home || listing.path.startsWith(`${listing.home}/`);
  const root = inHome ? listing.home : "";
  const parts = listing.path.slice(root.length).split("/").filter(Boolean);

  return (
    <div className={styles.crumbs}>
      <button type="button" onClick={() => onBrowse(inHome ? listing.home : "/")}>
        {inHome ? "~" : "/"}
      </button>
      {parts.map((part, i) => {
        const path = `${root}/${parts.slice(0, i + 1).join("/")}`;
        const last = i === parts.length - 1;
        return (
          <span key={path}>
            {(inHome || i > 0) && "/"}
            {last ? (
              <span className={styles.crumbLast}>{part}</span>
            ) : (
              <button type="button" onClick={() => onBrowse(path)}>
                {part}
              </button>
            )}
          </span>
        );
      })}
      <span className={styles.grow} />
      {listing.parent && (
        <button type="button" title="Parent folder" onClick={() => onBrowse(listing.parent!)}>
          ↑ up
        </button>
      )}
    </div>
  );
}

// Hint says what opening or creating will do.
function Hint({
  mode,
  listing,
  selected,
  target,
  taken,
}: {
  mode: "open" | "create";
  listing: DirectoryListing | null;
  selected: Directory | null;
  target: string | null;
  taken: boolean;
}) {
  if (mode === "create") {
    if (taken) return <p className={ui.error}>A folder with this name already exists here.</p>;
    if (!target) return <div className={styles.hint}>Type a name: the project is created in the folder shown.</div>;
    return (
      <div className={styles.hint}>
        Creates <code>{target}</code>, a git repository with an empty first commit, and its <code>.nobody/</code> with the director.
      </div>
    );
  }

  if (!target) return <div className={styles.hint}>Select a folder; double-click one to go inside.</div>;
  const dir = selected ?? (listing ? { name: baseName(listing.path), git: false, nobody: false, project: undefined } : null);
  if (!dir) return null;
  if (dir.project) return <div className={styles.hint}><b>{dir.name}</b> is already a known project: it opens in this window.</div>;
  if (dir.nobody)
    return (
      <div className={styles.hint}>
        <b>{dir.name}</b> already has a <code>.nobody/</code>: its teams and agents are used as they are.
      </div>
    );
  return (
    <div className={styles.hint}>
      <b>{dir.name}</b> gets a <code>.nobody/</code> with the director, and Claude Code trusts it.
      {selected && !selected.git && " It is not a git repository: features (branches and worktrees) need one."}
    </div>
  );
}

function baseName(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}
