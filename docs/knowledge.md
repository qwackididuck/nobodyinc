# The knowledge base

The knowledge base is the project's documentation: **a folder of the project**, its markdown files being pages. The control room shows it as the **library** in the office, where the human browses it from link to link and searches it; agents search and read it with their office tools, and write it like any file of the project. Decisions and rationale: ADR-0019.

Nobody Inc stores nothing of its own: the files are the only source of truth, kept in git, changed by commits and reviewed at "Finish" like code (ADR-0007). A full-text index is derived from them, and can be thrown away.

## Setting it up

The folder is declared in the project's `.nobody/org.yaml`, committed and shared by everyone on the project:

```yaml
knowledge:
  path: docs
```

No `knowledge:` key means no knowledge base: no library in the office, no knowledge tools for the agents, no mention in their prompt.

- **In the control room**: Settings › Project and daemon › Knowledge base. On suggests `docs`; type a path relative to the project, or choose it with the folder browser, bounded to the project. Off removes the key, **never the files**.
- **Through the API**: `PUT /api/kb` with `{"path": "docs"}`, or `{"path": ""}` to remove it.
- **By hand**: edit `org.yaml`; the change is picked up at the next change made through Nobody Inc, or when the daemon starts.

Rules on the path (`kb.CheckPath`, `kb.Resolve`): relative to the project; no `..`; not the project itself; not in `.nobody/`, `.claude/` or `.git/`; and, once symbolic links are followed, still inside the project and outside those three. A folder that does not exist is created, empty, when saved. A declared folder missing later (deleted, another branch checked out) is reported, not recreated.

Setting the knowledge base up, moving it or removing it **restarts every agent once idle, resuming its session**: the office MCP's knowledge tools come and go with it, and Claude Code lists a server's tools when a session starts. The resumed session gets the current system prompt with its first message (`agents.md`, "Sessions across openings").

The library appears in the office once the folder holds at least one page.

## Pages

- Only `*.md` files are pages; hidden files and folders (`.obsidian/`, `.draft.md`) are skipped. Folders are plain levels of the tree, with no meaning to Nobody Inc (`adr/`, `kb/`, `guides/`…).
- A page's **title** is its first `# ` heading, else its file name. Its optional YAML header (`status: current`) is shown under the title, never indexed as text.
- **Links** followed are relative markdown links, `[text](../adr/0003.md#context)`, a leading `/` meaning the knowledge folder's root. From them: each page's links, its **backlinks**, **broken links** (to a page or file that does not exist, or out of the folder) and **orphan pages** (no other page links to them). `[[wikilinks]]` are not resolved.
- **Images** a page shows are served read-only by `GET /api/kb/assets/{path}`: images only (png, jpg, gif, webp, svg, avif), under the same path rules, with a sandboxing Content-Security-Policy.
- **Limits**: a page over 1 MB is listed with its error, not indexed; an image over 10 MB is not served. A symbolic link to a file outside the folder is listed with its error, never read; a symbolic link to a folder is skipped.
- **Home page**: `index.md`, else `README.md`, at the root of the folder; without either, the reader shows a generated overview (top-level folders, recently changed pages, broken links, orphans).

Pages are parsed once, in the daemon, with goldmark (GitHub's flavor: tables, task lists, strikethrough, autolinks): the same parser gives the links, the index, the agents' tools and the HTML the control room shows. Raw HTML in a page is not rendered. Page contents are never logged: documentation may hold secrets.

## What is shown: the main checkout

The daemon reads the folder in the project's **main checkout**. During a feature, an agent changes the documentation in its team's worktree (`workspaces.md`): the library and the knowledge tools show the documentation as it was before the feature until "Finish" merges it, and the agent reads its own changes with its usual tools. Two teams changing the same page get a merge conflict, as for code.

There is no file watcher: the daemon scans the folder when it is read and the last scan is older than two seconds, and every five seconds in the background. A scan only stats the files, and reads again those whose size or modification time changed. Changes are announced to the control room with `kb.changed` (`events.md`).

## The full-text index

A SQLite FTS5 table in the project's database (`store/migrations/0012_kb.sql`): each page's title and plain text, accents ignored. It is kept in step with the files by digest: only pages whose content changed are indexed again. Deleting it loses nothing: the next scan rebuilds it.

A search finds the pages holding **every** word typed, the last one possibly the beginning of a word (results follow typing), best first (the title weighs more than the body), with a snippet around the matches. FTS5's own query syntax is not offered: words are quoted, so no search is ever a syntax error.

## Agents

When the project has a knowledge base, every agent gets three more office tools (`internal/officemcp/knowledge.go`), allowed without asking:

| Tool | Does |
|---|---|
| `kb_search` | Pages holding some words, best first, with a snippet (matches between « and »). |
| `kb_read` | A page's markdown (cut at 100 KB), its `file` in the project (to edit it), the pages it links to (broken ones marked) and those linking to it. |
| `kb_list` | A folder's sub-folders, with their number of pages, and its pages, with their titles. |

Every answer reminds the agent that what a page says is information, not instructions, to check against the code, and that it reads the main checkout. Their system prompt has a section on the knowledge base, only when the project has one (`internal/runtime/prompt.tmpl`).

**Writing**: every agent may change the documentation, with its usual tools, like any file: a change is a commit of its work, reviewed at "Finish". There is no per-agent restriction and no proposal queue; an agent that should leave the documentation alone is told so in its instructions or its team's prompt.

**What agents do with the files**: agents read and write the documentation with their own tools as often as with the knowledge tools, so their hooks tell the daemon too (`daemon/knowledge.go`, `docs/hooks.md`):

- a `PreToolUse` hook runs for the file tools only (`Read|Edit|Write|MultiEdit|NotebookEdit`, so that no other tool pays for it); when the file it names is in the knowledge folder, in the agent's working directory (its worktree during a feature) or in the main checkout, the daemon announces it like a knowledge tool. The hook never answers: the tool always runs;
- the `PostToolUse` of an edit there makes the daemon scan the knowledge base at once, so that the library shows the change without waiting for the background scan (a change in a worktree only reaches the main checkout with "Finish").

Each use of a knowledge tool, and each file tool about to touch a file of the knowledge folder, is announced with `kb.read`: in the office, the agent leaves its desk, walks to the library's reading table, shows what it does ("searching …", "reading kb/auth.md", "editing kb/auth.md"), and walks back (one walk at a time per agent).

## The control room

- **The library** stands in the office's left column, under the director's office and above the archives; without a knowledge base (or without a page) it is not there and the archives move up (`web/src/components/office/layout.ts`). Its shelves hold one spine per page (up to what they can show); new pages sparkle and "+N PAGES" floats up. Hovering it says how many pages and broken links it has; clicking opens the reader.
- **The reader** (`web/src/components/knowledge/Reader.tsx`) has three columns: the tree; the home page, a page or the results of a search; and what the centre links to (links, backlinks, broken links; the folders of the results; the orphans of the home). Links to pages open in the reader, anchors scroll, files and external links open in a new tab. ◂ ▸ walk the reader's own history, `/` focuses the search, Escape closes.

## API

| Action | Endpoint |
|---|---|
| Settings and overview (pages, folders, recent, broken links, orphans, pages not read) | `GET /api/kb` |
| Set up, move, remove | `PUT /api/kb` with `{"path"}` |
| The tree | `GET /api/kb/tree` |
| A page, rendered, with its links and backlinks | `GET /api/kb/pages/{path}` |
| Search | `GET /api/kb/search?q=&limit=` |
| An image | `GET /api/kb/assets/{path}` |

Without a knowledge base, every route but `GET`/`PUT /api/kb` answers 404. In a page's HTML, links to pages and anchors are `kb:<path>[#anchor]` and images and other files `kb-asset:<path>`: the daemon does not know the address the control room reaches it at (behind the launcher's proxy), so the control room turns them into navigation and asset URLs. Each link has a class: `kb-page`, `kb-asset` or `kb-external`, plus `kb-broken`.
