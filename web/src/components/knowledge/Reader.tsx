"use client";

// The library's reader (ADR-0019, docs/knowledge.md): the project's
// knowledge base in three columns over the control room, as in
// docs/mockups/knowledge/knowledge.html.
//
//   - left, the tree: folders (with their number of pages) and pages, the
//     current page's folders open;
//   - centre, the home page (index.md, README.md, or a generated overview),
//     a page, or the results of a search;
//   - right, what the centre links to: a page's links, backlinks and broken
//     links; the folders of the results; the orphan pages of the home.
//
// Pages come rendered by the daemon (raw HTML escaped). Their links to other
// pages are "kb:<path>[#anchor]" and open here; images are
// "kb-asset:<path>", turned into the assets endpoint's URL. ◂ ▸ walk the
// reader's own history; "/" focuses the search, Escape closes.

import { type MouseEvent, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import {
  getKnowledgePage,
  getKnowledgeTree,
  knowledgeAssetURL,
  type KnowledgeNode,
  type KnowledgeOverview,
  type KnowledgePage,
  type KnowledgeResult,
  type KnowledgeTree,
  searchKnowledge,
} from "@/lib/api";
import { useDaemonEvents, useLive } from "@/lib/live";
import styles from "./reader.module.css";

type View = { kind: "home" } | { kind: "page"; path: string; anchor?: string } | { kind: "search"; query: string };

// Prefixes of the links in the daemon's HTML (kb.PageLinkPrefix,
// kb.AssetLinkPrefix).
const PAGE_LINK = "kb:";
const ASSET_LINK = "kb-asset:";
// Heading ids get this prefix, so that a page's anchors never collide with
// the control room's own ids.
const ID_PREFIX = "kb-";
// Typing pauses this long before searching.
const SEARCH_DELAY_MS = 200;

interface Props {
  onClose: () => void;
}

export function Reader({ onClose }: Props) {
  const { knowledge } = useLive();
  const [tree, setTree] = useState<KnowledgeTree | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [history, setHistory] = useState<View[]>([{ kind: "home" }]);
  const [at, setAt] = useState(0);
  const [query, setQuery] = useState("");
  const searchBox = useRef<HTMLInputElement>(null);
  const view = history[at];

  // The tree, again when pages change.
  useEffect(() => {
    let cancelled = false;
    getKnowledgeTree()
      .then((t) => !cancelled && setTree(t))
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [version]);

  useDaemonEvents((e) => {
    if (e.type === "kb.changed") setVersion((v) => v + 1);
  });

  // go opens a view after the current one, dropping the views ahead of it.
  const go = useCallback(
    (next: View) => {
      setHistory((h) => [...h.slice(0, at + 1), next]);
      setAt((i) => i + 1);
    },
    [at],
  );

  // The search box follows the history: back on a search, it shows its
  // words again (React's "adjust state when props change" pattern).
  const [shownView, setShownView] = useState(view);
  if (shownView !== view) {
    setShownView(view);
    setQuery(view.kind === "search" ? view.query : "");
  }

  // Typing searches, after a pause; the results replace a previous search
  // in the history rather than piling up.
  useEffect(() => {
    const words = query.trim();
    if (!words || (view.kind === "search" && view.query === words)) return;
    const timer = setTimeout(() => {
      if (view.kind === "search") setHistory((h) => h.map((v, i) => (i === at ? { kind: "search", query: words } : v)));
      else go({ kind: "search", query: words });
    }, SEARCH_DELAY_MS);
    return () => clearTimeout(timer);
  }, [query, view, at, go]);

  // Clearing the box leaves the search for what was shown before it.
  function type(text: string) {
    setQuery(text);
    if (!text.trim() && view.kind === "search" && at > 0) setAt(at - 1);
  }

  // "/" focuses the search; Escape clears it, then closes the reader.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = document.activeElement === searchBox.current;
      if (e.key === "/" && !typing) {
        e.preventDefault();
        searchBox.current?.focus();
      }
      if (e.key === "Escape") {
        if (typing && query) type("");
        else onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const openPage = (path: string, anchor?: string) => go({ kind: "page", path, anchor });
  const current = view.kind === "page" ? view.path : view.kind === "home" ? (tree?.home ?? "") : "";

  return createPortal(
    <>
      <div className={styles.backdrop} onClick={onClose} />
      <div className={styles.reader} role="dialog" aria-modal="true" aria-label="Library">
        <header className={styles.head}>
          <div>
            <span className={styles.kicker}>LIBRARY</span>
            <h3 className={styles.headTitle}>{knowledge?.path ?? ""}/</h3>
          </div>
          <div className={styles.history}>
            <button type="button" className={styles.px} title="Back" disabled={at === 0} onClick={() => setAt(at - 1)}>
              ◂
            </button>
            <button type="button" className={styles.px} title="Forward" disabled={at >= history.length - 1} onClick={() => setAt(at + 1)}>
              ▸
            </button>
          </div>
          <label className={styles.search}>
            <span aria-hidden="true">⌕</span>
            <input ref={searchBox} value={query} placeholder="Search the documentation" aria-label="Search the documentation" onChange={(e) => type(e.target.value)} />
            <kbd>/</kbd>
          </label>
          <button type="button" className={styles.close} onClick={onClose}>
            CLOSE ✕
          </button>
        </header>
        <div className={styles.cols}>
          <nav className={styles.tree} aria-label="Pages">
            <p className={styles.colTitle}>
              {(knowledge?.path ?? "").toUpperCase()}/ · {tree?.root.pages ?? 0} PAGES
            </p>
            <ul>
              <li>
                <button type="button" className={styles.node} aria-current={view.kind === "home" ? "page" : undefined} onClick={() => go({ kind: "home" })}>
                  <span>⌂ Home</span>
                </button>
              </li>
              {tree?.root.children?.map((n) => <TreeNode key={n.path} node={n} current={current} onOpen={openPage} />)}
            </ul>
          </nav>
          {error ? (
            <div className={styles.centre}>
              <p className={styles.message}>{error}</p>
            </div>
          ) : view.kind === "search" ? (
            <SearchView query={view.query} version={version} onOpen={openPage} />
          ) : view.kind === "page" ? (
            <PageView path={view.path} anchor={view.anchor} version={version} onOpen={openPage} />
          ) : tree?.home ? (
            <PageView path={tree.home} version={version} onOpen={openPage} />
          ) : (
            <GeneratedHome overview={knowledge} onOpen={openPage} />
          )}
        </div>
      </div>
    </>,
    document.body,
  );
}

// TreeNode is a folder (open when it holds the current page, or when
// clicked) or a page.
function TreeNode({ node, current, onOpen }: { node: KnowledgeNode; current: string; onOpen: (path: string) => void }) {
  const holdsCurrent = node.dir && current.startsWith(`${node.path}/`);
  const [open, setOpen] = useState(holdsCurrent);
  const shown = open || holdsCurrent;

  if (!node.dir) {
    return (
      <li>
        <button
          type="button"
          className={styles.node}
          aria-current={node.path === current ? "page" : undefined}
          data-error={Boolean(node.error)}
          title={node.error ?? node.path}
          onClick={() => onOpen(node.path)}
        >
          <span>{node.error ? `${node.name} · ${node.error}` : (node.title ?? node.name)}</span>
        </button>
      </li>
    );
  }

  return (
    <li>
      <button type="button" className={styles.node} aria-expanded={shown} onClick={() => setOpen(!shown)}>
        <span>
          {shown ? "▾" : "▸"} {node.name}
        </span>
        <span className={styles.count}>{node.pages}</span>
      </button>
      {shown && (
        <ul>
          {node.children?.map((c) => <TreeNode key={c.path} node={c} current={current} onOpen={onOpen} />)}
        </ul>
      )}
    </li>
  );
}

interface PageViewProps {
  path: string;
  anchor?: string;
  version: number;
  onOpen: (path: string, anchor?: string) => void;
}

// PageView shows a page and, on the right, its links, backlinks and broken
// links.
function PageView({ path, anchor, version, onOpen }: PageViewProps) {
  const [page, setPage] = useState<KnowledgePage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const article = useRef<HTMLElement>(null);
  const centre = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    getKnowledgePage(path)
      .then((p) => {
        if (cancelled) return;
        setPage(p);
        setError(null);
      })
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [path, version]);

  const html = useMemo(() => (page ? prepareHTML(page.html) : ""), [page]);

  // Scroll to the anchor, or to the top of a new page.
  useEffect(() => {
    if (!page) return;
    const target = anchor ? article.current?.querySelector(`#${CSS.escape(ID_PREFIX + anchor)}`) : null;
    if (target) target.scrollIntoView({ block: "start" });
    else centre.current?.scrollTo({ top: 0 });
  }, [page, anchor]);

  // Links of the page: pages open here, files in a new tab, the rest
  // (external) in a new tab too, without telling it where it came from.
  function follow(e: MouseEvent) {
    const a = (e.target as HTMLElement).closest("a");
    const href = a?.getAttribute("href");
    if (!href) return;
    e.preventDefault();
    if (href.startsWith(PAGE_LINK)) {
      const [target, hash] = decodeURIComponent(href.slice(PAGE_LINK.length)).split("#");
      if (target === path && hash) {
        article.current?.querySelector(`#${CSS.escape(ID_PREFIX + hash)}`)?.scrollIntoView({ block: "start" });
        return;
      }
      if (target) onOpen(target, hash);
      return;
    }
    if (href.startsWith(ASSET_LINK)) {
      window.open(knowledgeAssetURL(decodeURIComponent(href.slice(ASSET_LINK.length))), "_blank", "noopener");
      return;
    }
    window.open(href, "_blank", "noopener,noreferrer");
  }

  const pageLinks = page?.links.filter((l) => !l.broken && (l.kind === "page" || l.kind === "external")) ?? [];
  const broken = page?.links.filter((l) => l.broken) ?? [];

  return (
    <>
      <div className={styles.centre} ref={centre}>
        {error && <p className={styles.message}>{error}</p>}
        {page && (
          <>
            <p className={styles.crumbs}>
              {crumbs(page.path)}
              <span> · as on the main checkout · changed {ago(page.modified)}</span>
            </p>
            {/* The daemon's HTML: raw HTML in the page is never rendered (goldmark), links rewritten. */}
            <article className={styles.page} ref={article} onClick={follow}>
              {page.header.length > 0 && (
                <div className={styles.header}>
                  {page.header.map((f) => (
                    <span key={f.key}>
                      {f.key}: {f.value}
                    </span>
                  ))}
                </div>
              )}
              <div dangerouslySetInnerHTML={{ __html: html }} />
            </article>
          </>
        )}
      </div>
      <aside className={styles.side}>
        {page && (
          <>
            <Side title={`LINKS (${pageLinks.length})`}>
              {pageLinks.map((l, i) => (
                <li key={i}>
                  {l.kind === "page" && l.path ? (
                    <button type="button" className={styles.sideLink} onClick={() => onOpen(l.path as string, l.anchor)}>
                      {l.title || l.text || l.path}
                    </button>
                  ) : (
                    <a className={styles.sideLink} href={l.target} target="_blank" rel="noopener noreferrer">
                      {l.text || l.target}
                    </a>
                  )}
                  <small>{l.kind === "external" ? "external ↗" : folderOf(l.path ?? "")}</small>
                </li>
              ))}
            </Side>
            <Side title={`BACKLINKS (${page.backlinks.length})`}>
              {page.backlinks.map((b) => (
                <li key={b.path}>
                  <button type="button" className={styles.sideLink} onClick={() => onOpen(b.path)}>
                    {b.title}
                  </button>
                  <small>{folderOf(b.path)}</small>
                </li>
              ))}
            </Side>
            {broken.length > 0 && (
              <Side title={`BROKEN (${broken.length})`}>
                {broken.map((l, i) => (
                  <li key={i}>
                    <span className={styles.sideLink} data-broken="true">
                      {l.text || l.target}
                    </span>
                    <small>{l.target}</small>
                  </li>
                ))}
              </Side>
            )}
          </>
        )}
      </aside>
    </>
  );
}

// SearchView shows the results of a search, best first, and the folders
// they are in, which narrow them down.
function SearchView({ query, version, onOpen }: { query: string; version: number; onOpen: (path: string) => void }) {
  const [results, setResults] = useState<KnowledgeResult[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [folder, setFolder] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    searchKnowledge(query)
      .then((r) => {
        if (cancelled) return;
        setResults(r);
        setError(null);
        setFolder(null);
      })
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [query, version]);

  const folders = new Map<string, number>();
  for (const r of results ?? []) folders.set(folderOf(r.path), (folders.get(folderOf(r.path)) ?? 0) + 1);
  const shown = (results ?? []).filter((r) => folder === null || folderOf(r.path) === folder);

  return (
    <>
      <div className={styles.centre}>
        {error && <p className={styles.message}>{error}</p>}
        {results && (
          <p className={styles.crumbs}>
            {results.length} result{results.length === 1 ? "" : "s"} for <b>&quot;{query}&quot;</b>
            {folder !== null && <> in {folder}</>}
          </p>
        )}
        <ul className={styles.results}>
          {shown.map((r) => (
            <li key={r.path}>
              <button type="button" className={styles.result} onClick={() => onOpen(r.path)}>
                <b>{r.title}</b>
                <small>{r.path}</small>
                <p>{r.snippet.map((s, i) => (s.match ? <mark key={i}>{s.text}</mark> : <span key={i}>{s.text}</span>))}</p>
              </button>
            </li>
          ))}
        </ul>
      </div>
      <aside className={styles.side}>
        {folders.size > 0 && (
          <Side title="IN FOLDERS">
            {[...folders].map(([f, n]) => (
              <li key={f}>
                <button type="button" className={styles.sideLink} onClick={() => setFolder(folder === f ? null : f)}>
                  {f}
                </button>
                <span className={styles.badge}>{n}</span>
              </li>
            ))}
          </Side>
        )}
      </aside>
    </>
  );
}

// GeneratedHome is the home of a knowledge base without index.md or
// README.md: its top-level folders, the pages changed last, and its health.
function GeneratedHome({ overview, onOpen }: { overview: KnowledgeOverview | null; onOpen: (path: string) => void }) {
  if (!overview) return <div className={styles.centre} />;
  const broken = overview.broken ?? [];
  const orphans = overview.orphans ?? [];
  const errors = overview.errors ?? [];

  return (
    <>
      <div className={styles.centre}>
        <p className={styles.crumbs}>
          {overview.path} / <b>home</b> · generated: no index.md or README.md in {overview.path}/
        </p>
        <article className={styles.page}>
          <h1>{overview.path}/</h1>
          <p>
            {overview.pages ?? 0} pages in {overview.folders?.length ?? 0} folders. Add an <code>index.md</code> to write your own home page.
          </p>
          <div className={styles.shelves}>
            {overview.folders?.map((f) => (
              <div key={f.path} className={styles.shelfCard}>
                <b>{f.path}</b>
                <small>
                  {f.pages} PAGE{f.pages === 1 ? "" : "S"}
                </small>
              </div>
            ))}
          </div>
          <h2>Recently changed</h2>
          <ul className={styles.recent}>
            {overview.recent?.map((p) => (
              <li key={p.path}>
                <button type="button" className={styles.pageButton} onClick={() => onOpen(p.path)}>
                  {p.title}
                </button>
                <small>
                  {folderOf(p.path)} · {p.modified ? ago(p.modified) : ""}
                </small>
              </li>
            ))}
          </ul>
          <h2>Health</h2>
          <p>
            {broken.length} broken link{broken.length === 1 ? "" : "s"} · {orphans.length} orphan page{orphans.length === 1 ? "" : "s"} (no page links to
            them) · {errors.length} page{errors.length === 1 ? "" : "s"} that could not be read
          </p>
        </article>
      </div>
      <aside className={styles.side}>
        {orphans.length > 0 && (
          <Side title={`ORPHANS (${orphans.length})`}>
            {orphans.map((p) => (
              <li key={p.path}>
                <button type="button" className={styles.sideLink} onClick={() => onOpen(p.path)}>
                  {p.title}
                </button>
                <small>{folderOf(p.path)}</small>
              </li>
            ))}
          </Side>
        )}
        {broken.length > 0 && (
          <Side title={`BROKEN (${broken.length})`}>
            {broken.map((l, i) => (
              <li key={i}>
                <button type="button" className={styles.sideLink} data-broken="true" onClick={() => onOpen(l.page)}>
                  {l.text || l.target}
                </button>
                <small>
                  in {l.page} → {l.target}
                </small>
              </li>
            ))}
          </Side>
        )}
      </aside>
    </>
  );
}

function Side({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <p className={styles.colTitle}>{title}</p>
      <ul>{children}</ul>
    </>
  );
}

// prepareHTML turns the daemon's HTML into what the reader shows: images of
// the knowledge folder get their URL, and heading ids their prefix.
function prepareHTML(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  for (const img of doc.querySelectorAll<HTMLImageElement>("img")) {
    const src = img.getAttribute("src") ?? "";
    if (src.startsWith(ASSET_LINK)) img.setAttribute("src", knowledgeAssetURL(decodeURIComponent(src.slice(ASSET_LINK.length))));
  }
  for (const el of doc.querySelectorAll("[id]")) el.id = ID_PREFIX + el.id;
  return doc.body.innerHTML;
}

// crumbs is a page's path as "docs / kb / auth.md", the file in bold.
function crumbs(path: string): ReactNode {
  const parts = path.split("/");
  return (
    <>
      {parts.slice(0, -1).map((p) => `${p} / `)}
      <b>{parts[parts.length - 1]}</b>
    </>
  );
}

// folderOf is a page's folder, "docs/" style ("/" for the root).
function folderOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "/" : `${path.slice(0, i)}/`;
}

// ago says how long ago a time was, roughly: "2 h ago", "yesterday".
function ago(iso: string): string {
  const minutes = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.round(hours / 24);
  if (days === 1) return "yesterday";
  if (days < 30) return `${days} days ago`;
  return new Date(iso).toLocaleDateString();
}
