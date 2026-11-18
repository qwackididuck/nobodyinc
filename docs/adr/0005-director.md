# ADR-0005 — The director ("god")

- Status: accepted; "no channel is needed" predates ADR-0009: flows are open, and the director is never blocked
- Date: 2026-11-13

## Context

So far the human drives the organization alone: they start features, write tickets for each team, recruit agents and relay questions between teams. We want an agent that does this on their behalf: it discusses a request with the human until the need and its acceptance criteria are clear, then turns it into work for the teams and follows it through.

Such an agent needs more than a team agent: it must talk to every team, create work for any of them, and change the organization itself. ADR-0003 and ADR-0004 require a decision for anything that widens an agent's rights.

## Decision

### 1. One director per project, outside the teams

The director is an agent like the others (a definition file, a Claude Code session in a PTY, hooks, permissions), with no team:

- its file is `<project>/.nobody/director/<name>.md`, at most one; its **address is its name** (`god` by default, the user picks another by naming the file differently);
- it always works in the project's main checkout, even while a feature is active: it reads and plans, it does not deliver code. Starting a feature therefore does not restart it, so it keeps the conversation in which the feature was agreed;
- `nobodyd init` recruits it from the built-in `god` template; an existing project recruits it from the Recruitment tab or `POST /api/director`.

  *Amended 2026-11-16*: the director is **always present**. It is no longer in the catalog: `nobodyd init` writes it (and nothing else: the user builds the teams), and the daemon writes it at startup when a project has none (`org.EnsureDirector`). `POST /api/director` is gone, and dismissing the director or putting it on leave is refused (409); it can only be edited. It may also create teams (`team_create`). It is not a source of knowledge about the project: the agents' routing rule treats a question to the director like a question to the human (`docs/agents.md`, "Whom to ask").

### 2. Its rights

- **Messages**: like the human, it may write to anyone, and anyone may write to it; no channel is needed.
- **Tickets**: like the human, it is privileged in the ticket service: it creates tickets for any team (with dependencies, acceptance criteria and an assignee) and changes the status of any ticket.
- **The organization**: it recruits, creates, edits, puts on leave and dismisses agents **freely**, through office MCP tools that call the same staffing service as the API. The daemon still writes `.nobody/`, never the director (ADR-0004). It cannot change its own definition, leave or dismiss itself: like every agent, it must not widen its own rights; the human does it. The user chose not to add an approval step: the director's prompt asks it to explain its own initiatives, and to show a definition written from scratch before creating the agent.
- **Features**: it **proposes**, the human **decides**. `feature_propose` records the feature as `proposed` (nothing happens in git or to the agents) and sends the human a `propose` message; the human starts it or refuses it (with a reason) from the control room, and the director gets the answer as `agree` or `refuse` mail. Using the existing message acts keeps a single protocol, and proposals are stored, so they survive a daemon restart.

Its permission profile for Claude Code tools stays its own choice in its definition (`default` in the template): its work goes through the office tools, which are allowed without asking.

## Consequences

- The office MCP server exposes the director's tools (`feature_propose`, `org_overview`, `template_list`, `agent_*`) only at the director's address.
- Anything that can reach the office MCP endpoint at the director's address acts as the director: like the rest of the daemon's API, it is only protected by listening on loopback until the API is authenticated (VPS mode).
- A feature's status (`proposed`, `started`, `refused`) is stored with it (migration `0004`).
- Guide: `docs/god.md`.
