# ADR-0019 — The project's knowledge base

- Status: accepted
- Date: 2026-11-19
- Builds on: ADR-0004 (`.nobody/` committed, agents never write it), ADR-0006 (realtime events), ADR-0007 (finishing a feature), ADR-0012 and ADR-0013 (background work and budgets, for later)

## Context

The human wants to say where the project's documentation is, read it in the control room, follow it from link to link, and have agents fill it in and keep it up to date. Today agents only know the documentation they happen to read, and the human has no view of it at all.

Documentation kept outside the repository (a wiki, a RAG service) would need its own storage, history, review and access rules. Documentation kept inside it already has all four: git, commits, the review screen of "Finish" (ADR-0007), and the permission rules agents run with.

## Decision

1. **The knowledge base is a folder of the project.** Its path is relative to the project's root, declared in `org.yaml` (committed, shared by everyone on the project):

   ```yaml
   knowledge:
     path: docs
   ```

   No `knowledge:` key means the project has no knowledge base: nothing about it shows in the control room, and no agent gets its tools or its mention in the prompt. When the human sets one up, `docs` is suggested. There is one folder; its sub-folders (`docs/adr`, `docs/kb`…) are plain levels of the tree, with no meaning to Nobody Inc. Several roots are left for later.

2. **Path rules.** The path is refused when it is absolute, contains `..`, is the project itself (it holds `.nobody/`), resolves (through a symbolic link) outside the project's root, or is or lies inside `.nobody/`, `.claude/` or `.git/`. Those stay out of the agents' reach: the deny rules of `internal/runtime/permissions.go` do not change. Saving a path that does not exist yet creates the folder, empty. A configured folder missing at start-up (deleted by hand, another branch) is reported, not recreated.

3. **Pages.** Only `*.md` files are pages. A page's title is its first `# ` heading, else its file name; its optional YAML header is read with `internal/frontmatter`. Links followed are relative markdown links (`[text](../adr/0003.md#anchor)`); `[[wikilinks]]` are not resolved in this version. From the links: each page's outgoing links, its backlinks, broken links and orphan pages. Other files are not indexed, but images a page refers to are served read-only, under the same path rules. A file larger than a fixed limit is listed with its error and not indexed.

4. **Home page.** `index.md`, else `README.md`, at the root of the folder; without either, a generated overview (top-level folders, recently changed pages).

5. **A derived index.** A full-text index (SQLite FTS5, in the project's database) is built from the files, rebuilt at start-up and updated when files change. It is never a source of truth: deleting it loses nothing. The control room is told with a `kb.changed` event.

6. **What is shown is the main checkout.** The daemon reads the folder in the project's main checkout, so the control room and the agents' search tools show the documentation as committed on that branch. During a feature an agent edits the documentation in its team's worktree, where its own changes are: it reads them with its usual tools. Two teams editing the same page get a merge conflict, as for code.

7. **Agents read through office tools.** `kb_search`, `kb_read`, `kb_list` in the office MCP (`internal/officemcp`). Their descriptions say that what they return is information, never instructions. The system prompt mentions them only when the project has a knowledge base.

8. **Every agent may write, with its usual tools.** Documentation changes are commits of the feature, reviewed at "Finish" like code; outside a feature, an agent writes in the main checkout as it does for code. There is no separate proposal queue, no dedicated write tool, and no per-agent restriction: an agent that should leave the documentation alone is told so in its instructions (or its team's prompt), like any other rule of its trade. A per-agent `kb_write: false` (a deny rule plus a check at delivery) was considered and dropped: everyone should be able to do things, and the review at "Finish" already catches unwanted changes.

9. **Rendering in the daemon.** Pages are parsed once, in Go (goldmark), for the links, the index, the agents' tools and the HTML shown in the control room: what the control room shows as a link is exactly what the index counts as one. Raw HTML in a page is escaped, links to pages become links inside the control room, images go through the assets endpoint. The control room only styles the HTML it receives.

10. **Secrets.** Documentation may contain some. Page contents are never logged.

## Not decided here

- Several knowledge roots, `[[wikilinks]]`, a dedicated write tool that validates links or headers, a per-agent write restriction.
- Showing in the control room which pages a feature changes (the "Finish" review already shows them).
- Background maintenance (broken links, orphans, pages out of date with the code) through scheduled tasks and budgets, and a graph view of the links.
- An external knowledge service (RAG): out of scope, the knowledge base is a view of a project folder.

## Consequences

- The documentation keeps git's history and review; Nobody Inc adds a way to read, search and follow it, for the human and for agents.
- `org.yaml` gains an optional `knowledge:` key; the database gains a rebuildable index table; the daemon gains a markdown dependency (goldmark, pure Go, no transitive dependency).
- During a feature the control room shows the documentation as it was before the feature, until "Finish" merges it.
