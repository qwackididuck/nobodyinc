# Projects: a launcher, one daemon per project, one window per project

Nobody Inc drives several projects from one control room. `nobodyd` is a **launcher**: it knows the projects, lets the control room create, open and delete them, and runs one **project daemon** per open project, as a child process, for as long as a window shows it. Decisions and rationale: ADR-0014.

## Overview

```
 Browser windows (web/)            one window = one project: /?project=<id>
   │ REST + SSE: /api/... (launcher), /p/<id>/api/... (a project)
   │ WebSockets: /api/projects/<id>/attach (holding), /p/<id>/api/agents/…/terminal
   ▼
┌──────────────── nobodyd: the launcher (internal/launcher), 127.0.0.1:7420 ───────────────┐
│ projects   GET/POST /api/projects, DELETE /api/projects/{id}, registry projects.yaml     │
│ folders    GET /api/fs/dirs                                                              │
│ holds      GET /api/projects/{id}/attach (WebSocket), grace period                       │
│ proxy      /p/{id}/... → the project daemon's /...                                       │
└───────┬─────────────────────────────────────┬────────────────────────────────────────────┘
        │ starts, hands over a socket,        │
        │ stops (SIGTERM)                     │
        ▼                                     ▼
 nobodyd project -project shop …       nobodyd project -project blog …
 127.0.0.1:<port> (daemon.Daemon)      127.0.0.1:<port>
   ▲ hooks, MCP (NOBODY_DAEMON_URL)
   │
 claude (one per agent)
```

A project daemon is the daemon described in `architecture.md`, unchanged inside: agents, hooks and MCP clients talk to it directly, on its own port. Only the control room goes through the launcher.

## Where projects live

```
<project>/.nobody/                committed with the code: the organization (agents.md)
~/.nobody/                        NOBODY_HOME replaces it
  settings.yaml, catalog/         the user's settings and templates, for every project
  projects.yaml                   the known projects, written by the launcher only
  projects/<id>/                  one project's state on this machine
    nobody.db                     database: mail, tickets, features, spend
    worktrees/, mcp/, skills/     the teams' worktrees, MCP configurations, skills staging
    daemon.lock                   held by the running project daemon
```

A project's **id** is its folder's name and a hash of its absolute path, `shop-6e403867`: two folders with the same name never share a state directory (`projects.ID`). `projects.yaml` lists, for each known project, its id, its path, and when a window last opened it. A project whose folder disappeared stays listed as missing until the human removes it.

## Opening and creating a project

The projects page (`Lobby.tsx`) lists the known projects as buildings: lit windows when the project is open in a window, with its agents at work, this month's spend and the active feature (read from its daemon through the launcher every 10 seconds). It is also where projects are opened, created and deleted; inside a project, the project's name in the top bar opens the same actions (`ProjectMenu.tsx`).

- **Open folder…** browses the machine through the launcher (`GET /api/fs/dirs`: names of sub-folders only, hidden ones left out; whether each is a git repository, has a `.nobody/`, or is a known project). Opening a folder (`POST /api/projects` with its path) adds it to the registry and, if it has no `.nobody/` yet, **sets it up** (`projects.SetUp`): `.nobody/` with the director alone (`god.md`), the user's catalog directory and settings file if missing, and Claude Code's trust of the folder (`runtime.Trust`, as accepting Claude Code's trust question would: `workspaces.md`). A folder that already has a `.nobody/` keeps it as it is.
- **New project** (`POST /api/projects` with `create: true`) creates the folder (its parent must exist), makes it a git repository with an empty first commit signed by Nobody Inc (a feature needs a base branch with a commit), then sets it up.

Opening a project from the page shows it in this window; Cmd/Ctrl-click opens it in a new window.

## One window, one project

A window shows one project, named in its URL (`/?project=<id>`). It **holds** the project through a WebSocket to the launcher (`GET /api/projects/{id}/attach?window=<window id>`, `lib/window.ts`), and the project runs only while a window holds it (`launcher/leases.go`):

| What happens | What the launcher does |
|---|---|
| A window opens the project | Starts its daemon unless it runs, and tells the window its state: `starting`, then `running`. |
| The window's connection drops (closed, reloaded, network lost) | Keeps the project running for a **grace period of 15 seconds**. The same window connecting again within it keeps it, as if nothing happened. After it, the daemon is stopped. |
| The window switches project | The window sends `release`: the daemon stops at once, without grace period. |
| A second window opens a project held by a connected window | Refuses it (`held`, close code 4409): "This project is open in another window", with **Open here** (connect again with `takeover=true`) or **Back to projects**. A hold in its grace period is free to take, without takeover. |
| A window takes the project over | The window that had it gets `taken` (close code 4409): "Opened in another window", with **Take it back**. The daemon keeps running. |
| The daemon stops on its own (crash, invalid `.nobody/`) | Tells the window `stopped` with the reason (the daemon's last log line). The control room shows a red band; **Restart** sends `start`. |

Details:

- The window's id is kept in `sessionStorage`: it survives a reload (so the reload gets the hold back), and is per tab. A duplicated tab copies it; the newest connection then wins, and the other tab shows "Opened in another window".
- The launcher pings every window every 10 seconds: a connection that stopped answering counts as dropped.
- A **locked screen** changes nothing: the machine runs and the connection is on loopback. During **sleep**, the window, the launcher and the daemons are frozen together: the connection does not drop and the grace period does not run out. A browser that discards an inactive tab (Chrome's memory saver) closes its connection: the project stops after the grace period, and opens again when the tab comes back.
- The window reconnects on its own when the connection drops: within the grace period it keeps its hold; after it, the project was stopped, and the window opens it again (it is still that window's project).
- Switching project while agents work asks first, naming them; closing the tab gets the browser's own "Leave site?" prompt (`LeaveGuard`).
- The projects page holds nothing.

## What stops with a project

Stopping a project stops its daemon, and with it every agent's Claude Code session. Mail, tickets, the active feature, scheduled tasks, budgets and spend are kept, in the database and in `.nobody/`. At the next opening the agents start again, on new sessions (they find their work through their tickets, but not their conversation). Scheduled tasks run only while the project is open: an occurrence that falls while it is closed is lost (ADR-0012).

## Deleting a project

**Delete project…** (on a project's card, or in the project menu) removes Nobody Inc from the project, **never its code** (`DELETE /api/projects/{id}`, `launcher/delete.go`). The human types the project's name to confirm. In this order, so that a refusal changes nothing:

1. refused while another window holds the project; the window that holds it may delete it, and goes back to the projects page;
2. if a feature is not finished (an active feature), the deletion is refused with the feature, and the human chooses:
   - **Merge as it is** (`feature=merge`): the feature is finished anyway (ADR-0007, `force`): unfinished tickets canceled, local merge into the base branch, nothing pushed. If the merge is refused (a conflict, uncommitted changes in the project), nothing is deleted and the reason is shown;
   - **Delete anyway** (`feature=discard`): the teams' worktrees are removed, with their uncommitted changes; the branches stay;
   - **Cancel**.

   The running daemon answers when the project is open; otherwise the launcher runs the daemon without its agents (`-start=false`) to read the feature and finish it, then stops it;
3. windows lose the project and may not open it again until the deletion ends; its daemon is stopped;
4. the teams' worktrees are removed (`git worktree remove`, so that git forgets them), then `.nobody/`, the state directory and the registry entry.

What stays: the code, **every git branch** (`nobody/*` included: they are code), the user's templates and settings. If `.nobody/` was committed, its deletion shows in `git status`, for the user to commit or not.

## How a project daemon runs

`nobodyd project -project <dir> -listen-fd 3 …` is started by the launcher only (`launcher/process.go`, `cmd/nobodyd/project.go`):

- **Its port**: the launcher opens a listening socket on 127.0.0.1 (a free port) and hands it over as file descriptor 3. No port can be taken in between, and the daemon knows its URL (`NOBODY_DAEMON_URL`, for hooks and MCP) before its agents start.
- **Never alone**: its standard input is a pipe from the launcher. When the launcher dies, even killed, the pipe closes and the daemon stops, with its agents.
- **One per project**: it locks `daemon.lock` in the project's state directory (`projects.Lock`, an operating-system lock that goes away with the process): a second daemon on the same project refuses to start.
- **Stopping**: the launcher sends SIGTERM (the daemon stops its agents and waits for its background work, such as a feature's clean-up) and kills it after 30 seconds.
- **Starting**: the launcher waits for its `/healthz` (at most 2 minutes). A daemon that exits during its start reports why with its last log line.
- **Its options** come from the launcher's flags (`-claude`, `-hook`, `-start`, `-origins`), and `NOBODY_HOME` from the launcher. With `-start=false`, agents neither start with the daemon nor restart when a feature starts or ends (`daemon.Options.StartAgents`).
- **Its logs** go to the launcher's standard output, each line with the project's id.

## The control room

- `lib/api.ts` sends every project call to `/p/<id>/api/...` (`setProject`, called once by the window); `lib/events.ts` opens `/p/<id>/api/events`; terminals connect to `ws://<launcher>/p/<id>/api/agents/<address>/terminal`.
- `lib/launcher.ts` is the client of the launcher's own routes; `lib/window.ts` holds the window's project (`useHold`).
- `components/projects/`: the projects page (`Lobby`), the window and its states (`ProjectWindow`), the project menu (`ProjectMenu`), the folder browser (`FolderBrowser`), the delete dialog (`DeleteProject`).
- In development, `next.config.ts` proxies `/api/*` and `/p/*` to the launcher; WebSockets go straight to it (`NEXT_PUBLIC_NOBODY_DAEMON_WS`), which allows the control room's origin (`-origins`).
- Mock-ups: `docs/mockups/multi-project/projects.html`.

## Developing and testing

- `task dev:daemon` runs the launcher; `task dev:daemon ARGS="-start=false"` opens projects without starting (or paying for) agents, `ARGS="-claude <built test/fakeclaude>"` with free fake agents. Then `task dev:web`, and open a folder from the projects page.
- Opening a folder trusts it in the real `~/.claude.json`, and the registry is the real `~/.nobody/projects.yaml`: try things on throwaway folders, or with `NOBODY_HOME` and `CLAUDE_CONFIG_DIR` pointing to throwaway directories.
- `internal/projects` and `internal/launcher` (holds on a `schedule.ManualClock`) have unit tests; `test/e2e/launcher_test.go` runs the real launcher with real project daemons (the `nobodyd` binary) and fakeclaude: two projects side by side, closing a window, the second window and the takeover, deleting with an unfinished feature (discard and merge).

| Code | Role |
|---|---|
| `internal/projects` | `~/.nobody` and state directories, the lock, the registry, setting a folder up, removing Nobody Inc from a project. |
| `internal/launcher` | The launcher: routes, holds and their grace period, project daemons as child processes, the proxy, deletion. |
| `cmd/nobodyd` | `main.go` (the launcher), `project.go` (a project daemon). |
