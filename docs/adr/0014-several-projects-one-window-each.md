# ADR-0014 — Several projects: a launcher, one daemon per project, one window per project

- Status: proposed
- Date: 2026-11-18

## Context

Until now, one daemon runs one project: `nobodyd -project <dir>`, created beforehand with `nobodyd init`. Running a second project means a second daemon on another address (`NOBODY_ADDR`), a second Next.js server pointed at it, and origins to align by hand. Nothing in the control room says which project it shows, and creating a project needs the terminal.

The human wants to drive every project from the interface: create one, open one (and set Nobody Inc up on a folder that has no `.nobody/` yet), delete one, and switch between them, with the daemons started and stopped behind the scenes. The target is a desktop application (Electron-like) that can open several windows at once, each on its own project. Running projects cost money and memory: a project nobody looks at should not keep running.

A daemon is built around one project: its organization, database, event bus, agents' hooks and MCP clients (which reach the daemon through `NOBODY_DAEMON_URL`), schedules and budgets. Hosting several projects in one process would mean carrying the project through all of them, and a crash in one project would stop all the others.

## Decision

### 1. A launcher, and one daemon process per project

`nobodyd` becomes a **launcher**: it listens on 127.0.0.1:7420, keeps the list of known projects, serves the project-level endpoints (point 6) and starts one **project daemon** per open project: a child `nobodyd` process, unchanged inside (`daemon.Daemon`), on a port of its own.

- The launcher opens the child's listening socket itself (127.0.0.1, port 0) and hands it to the child as an inherited file descriptor: no port race, and the child knows its own URL before starting its agents (`NOBODY_DAEMON_URL` for hooks and MCP is the child's address, as today).
- The child stops when its launcher dies: it watches a pipe from the launcher and shuts down when it closes. No daemon is ever left running alone.
- A project daemon takes a lock in its state directory (`~/.nobody/projects/<id>/daemon.lock`): one project never runs twice, whoever starts it.
- The control room reaches a project through the launcher: `/p/<id>/api/...` is proxied to the child's `/api/...`, REST, the event stream (SSE) and the terminals' WebSockets alike. Agents keep talking to their own daemon directly.
- The project daemon mode is internal (`nobodyd project ...`, used only by the launcher). `nobodyd init` and `nobodyd -project` are removed: the control room creates and opens projects. If Nobody Inc runs in the cloud one day, the same screens are its onboarding.

### 2. The registry of known projects

`~/.nobody/projects.yaml` (written by the launcher only, crash-safe) lists the known projects: id, absolute path, display name, when it was last opened. The id is the name of the project's state directory, `<folder>-<hash of the path>` (ADR-0004), already used by the control room (`project_id`). A project whose folder disappeared stays listed as missing, until the human removes it.

### 3. One window, one project

- The project is in the window's URL (`/?project=<id>`), so each window shows its own.
- A window that shows a project **holds** it: it keeps a WebSocket open to the launcher (`/api/projects/<id>/attach`, with an id the window keeps in `sessionStorage`). Holding a project starts its daemon if it is not running.
- **A project runs only while a window holds it.** When its window is closed, the connection drops; after a grace period (15 s) without the window coming back, the launcher stops the project daemon and its agents. A crash of the window or of the application drops the connection too.
- **Switching project in a window** releases the current one at once (no grace period) and opens the other.
- **A project is held by one window at most.** A second window asking for it is refused (409) and shows "This project is open in another window", with "Open here" (it takes the project over; the other window shows "Open elsewhere") or "Back to projects". The desktop application, which knows its windows, will bring the existing window to the front instead.
- The window reconnects on its own when the connection drops. Within the grace period it gets the same hold back and nothing stopped; after it, the project was stopped, and the window opens it again, since it is still that window's project.
- Pings every 10 s detect a half-dead connection. A locked screen changes nothing (the machine runs, the connection is on loopback). During sleep every process is frozen, the window and the launcher alike: the connection does not drop, and the grace period, measured by the launcher, does not run out while asleep.
- Closing a window while agents work: the control room warns before leaving (the browser's own prompt; the desktop application can ask "Close and stop / Cancel").
- The projects page (point 5) holds no project.

### 4. What stops with a project

Stopping a project stops its daemon and every agent session; mail, tickets, the active feature, schedules and budgets are kept (SQLite and `.nobody/`). At the next opening the agents start again, on new Claude Code sessions until sessions are resumed after a daemon restart (roadmap, Runtime). Schedules run only while the project is open: ADR-0012 already loses the occurrences that fall while the daemon is stopped.

### 5. Creating, opening and deleting projects

- The control room browses the machine's folders through the launcher (`GET /api/fs/dirs`): names of the sub-folders, whether each is a git repository, already has a `.nobody/`, or is a known project. Listing only, nothing is read inside files.
- **Open** a folder: it joins the registry. A folder without `.nobody/` is set up first, as `nobodyd init` did: `.nobody/` with its director and no team, the user's catalog and settings if missing, and Claude Code's trust of the folder (`runtime.Trust`), so that agents do not stop on the trust question.
- **New project**: creates the folder, runs `git init`, then sets it up as above.
- **Delete** a project removes Nobody Inc from it, never the code: the project's `.nobody/` (a tracked deletion if it was committed, left to the user to commit), its state directory `~/.nobody/projects/<id>/` (database, worktrees, MCP and skills staging) and its registry entry. The git branches stay, `nobody/*` included: they are code. Delete is refused while another window holds the project; from the project's own window, the window releases it and goes back to the projects page.
- **A feature that is not finished** (an active feature) stops the deletion and the human chooses:
  - **Merge as it is**: the feature is finished anyway (ADR-0007, `force`): unfinished tickets canceled, local merge into the base branch. On a conflict or a refusal (uncommitted changes in the project), nothing is deleted and the reason is shown.
  - **Delete anyway**: the teams' worktrees are removed (their uncommitted changes are lost, the control room says so), their branches and the feature branch stay.
  - **Cancel**.
  To check and finish the feature, the launcher runs the project daemon without starting its agents (`-start=false`), then stops it.

### 6. Launcher API

Described in `api/openapi.yaml` with the project daemon's, under its own tag: `GET/POST /api/projects` (list; open or create), `DELETE /api/projects/{id}` (with the choice about an unfinished feature), `GET /api/projects/{id}/attach` (WebSocket), `GET /api/fs/dirs`. The cross-site guard and allowed origins apply to the launcher as to the daemon; the launcher hands its origins to the children.

## Not decided here

- The desktop application itself (Electron, Wails, Tauri): a separate feature. This ADR only needs it to bring an existing window to the front and to ask before closing a window whose agents work.
- Resuming agents' sessions after a daemon restart (roadmap, Runtime), made more useful by this ADR.
- Stopping an idle project while its window stays open.
- Running projects with no window (background projects, schedules that must always run).

## Consequences

- The control room becomes multi-project: a projects page, a project menu in the top bar, and the project's name always shown.
- Closing a window frees what its project used; nothing runs that no window shows.
- The project daemon is unchanged inside; agents, hooks and MCP clients see no difference. Every window's API calls go through the launcher's proxy.
- `nobodyd init` and `nobodyd -project` disappear; `task dev:daemon` runs the launcher.
- A project's schedules and agents stop when its window closes: the human must keep the window open for work to go on.
- New files: `~/.nobody/projects.yaml`, a lock in each project's state directory. No new dependency (`net/http/httputil` proxies WebSockets).
