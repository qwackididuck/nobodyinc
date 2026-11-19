"use client";

// The knowledge base option of Settings › Project and daemon (ADR-0019):
// Off, or On with a folder of the project ("docs" suggested), chosen by hand
// or with a folder browser bounded to the project. Saving writes
// "knowledge: {path}" in .nobody/org.yaml (a missing folder is created,
// empty); Off removes the key, never the files. Below: the pages found and
// what is wrong with them.

import { type FormEvent, useCallback, useEffect, useState } from "react";

import { setKnowledge } from "@/lib/api";
import { type DirectoryListing, listDirs } from "@/lib/launcher";
import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import styles from "./knowledge.module.css";

// Folders the path rules refuse (kb.CheckPath): shown, not chosen.
const REFUSED = new Set([".nobody", ".claude", ".git"]);

export function KnowledgeSetting() {
  const { knowledge, settings } = useLive();
  const [enabling, setEnabling] = useState(false);
  const [draft, setDraft] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!knowledge || !settings) return <p className={ui.sub}>Loading…</p>;

  const on = knowledge.path !== "" || enabling;
  const path = draft ?? (knowledge.path || knowledge.suggested);

  async function save(next: string) {
    setBusy(true);
    try {
      await setKnowledge(next);
      setError(null);
      setDraft(null);
      setEnabling(false);
      setBrowsing(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    void save(path.trim());
  }

  function turnOff() {
    if (knowledge?.path) void save("");
    setEnabling(false);
    setDraft(null);
    setBrowsing(false);
  }

  return (
    <div className={styles.knowledge}>
      <div className={ui.seg} role="group" aria-label="Knowledge base">
        <button type="button" aria-pressed={!on} disabled={busy} onClick={turnOff}>
          Off
        </button>
        <button type="button" aria-pressed={on} disabled={busy} onClick={() => setEnabling(true)}>
          On
        </button>
      </div>

      {on && (
        <form className={styles.field} onSubmit={submit}>
          <label className={styles.path}>
            <span>{settings.project_dir.split("/").pop()}/</span>
            <input value={path} aria-label="Knowledge base folder" spellCheck={false} onChange={(e) => setDraft(e.target.value)} />
          </label>
          <button type="button" className={ui.btn} onClick={() => setBrowsing(!browsing)}>
            Browse…
          </button>
          <button type="submit" className={ui.btnPrimary} disabled={busy || !path.trim() || path.trim() === knowledge.path}>
            Save
          </button>
        </form>
      )}

      {on && browsing && (
        <ProjectFolders
          root={settings.project_dir}
          start={path}
          onChoose={(rel) => {
            setDraft(rel);
            setBrowsing(false);
          }}
          onCancel={() => setBrowsing(false)}
        />
      )}

      {error && <p className={ui.error}>{error}</p>}
      {knowledge.path && !enabling && <KnowledgeStatus />}
    </div>
  );
}

// KnowledgeStatus sums up the knowledge base: its pages, its folders, and
// the broken links and pages that could not be read, in detail on demand.
function KnowledgeStatus() {
  const { knowledge } = useLive();
  if (!knowledge) return null;

  if (knowledge.error) return <p className={ui.error}>{knowledge.error}</p>;
  if (knowledge.exists === false) return <p className={styles.status}>The folder {knowledge.path}/ is missing on this branch: nothing to show.</p>;

  const broken = knowledge.broken ?? [];
  const errors = knowledge.errors ?? [];
  const pages = knowledge.pages ?? 0;

  return (
    <div className={styles.status}>
      <span className={styles.ok}>■ {pages} page{pages === 1 ? "" : "s"}</span> in {knowledge.folders?.length ?? 0} folders
      {pages === 0 && <> · no page yet: the library opens with the first one</>}
      {(broken.length > 0 || errors.length > 0) && (
        <>
          {" · "}
          <span className={styles.warn}>
            {broken.length} broken link{broken.length === 1 ? "" : "s"} · {errors.length} page{errors.length === 1 ? "" : "s"} not read
          </span>
          <details>
            <summary>Details</summary>
            {broken.map((l, i) => (
              <div key={`b${i}`}>
                {l.page} → <code>{l.target}</code> (missing)
              </div>
            ))}
            {errors.map((p) => (
              <div key={p.path}>
                {p.path}: {p.error}
              </div>
            ))}
          </details>
        </>
      )}
    </div>
  );
}

interface FoldersProps {
  // The project's folder, absolute; the browser never goes above it.
  root: string;
  // Where to start, relative to root, when it exists.
  start: string;
  onChoose: (relative: string) => void;
  onCancel: () => void;
}

// ProjectFolders browses the project's folders through the launcher
// (GET /api/fs/dirs), from the project's root down; .nobody, .claude and .git
// are shown greyed out.
function ProjectFolders({ root, start, onChoose, onCancel }: FoldersProps) {
  const [rel, setRel] = useState("");
  const [listing, setListing] = useState<DirectoryListing | null>(null);
  const [error, setError] = useState<string | null>(null);

  // list lists a folder relative to root, with its relative path.
  const list = useCallback(
    (next: string) => listDirs(next ? `${root}/${next}` : root).then((l) => ({ l, next })),
    [root],
  );

  function show({ l, next }: { l: DirectoryListing; next: string }) {
    setListing(l);
    setRel(next);
    setError(null);
  }

  // browse opens a folder clicked in the list.
  const browse = (next: string) => {
    list(next)
      .then(show)
      .catch((e: Error) => setError(e.message));
  };

  // The start folder first, else the root (it may not exist yet).
  const [first] = useState(() => start.replace(/\/+$/, ""));
  useEffect(() => {
    list(first)
      .catch(() => list(""))
      .then(({ l, next }) => {
        setListing(l);
        setRel(next);
      })
      .catch((e: Error) => setError(e.message));
  }, [list, first]);

  const up = rel.includes("/") ? rel.slice(0, rel.lastIndexOf("/")) : "";

  return (
    <div className={styles.browser}>
      <header>
        <button type="button" className={ui.link} disabled={!rel} onClick={() => browse(up)}>
          ▴ up
        </button>{" "}
        {root.split("/").pop()}/{rel}
      </header>
      {error && <p className={ui.error}>{error}</p>}
      <ul>
        {listing?.dirs.map((d) => {
          const child = rel ? `${rel}/${d.name}` : d.name;
          const refused = !rel && REFUSED.has(d.name);
          return (
            <li key={d.path}>
              <button type="button" disabled={refused} title={refused ? "Not documentation" : `Open ${child}`} onClick={() => browse(child)}>
                {d.name}
              </button>
            </li>
          );
        })}
        {listing && listing.dirs.length === 0 && <li className={styles.empty}>No sub-folder</li>}
      </ul>
      <footer>
        <button type="button" className={ui.btn} onClick={onCancel}>
          Cancel
        </button>
        <button type="button" className={ui.btnPrimary} disabled={!rel} onClick={() => onChoose(rel)}>
          Choose {rel || "a folder"}
        </button>
      </footer>
    </div>
  );
}
