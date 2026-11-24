# Plain terminals (shells)

The control room has a terminal drawer along the bottom of the window, to run a command in the project without leaving it (ADR-0021). Decisions are in the ADR; this page is how it works.

## What the human sees

- A **Terminal** button in the top bar, and `Ctrl+\``, show or hide the drawer from any tab. Hiding it does not end the shells: they keep running, with their screens.
- One tab per shell, `+` for another one (8 at most), `×` to close one. Closing the last one hides the drawer, and so does typing `exit` in it.
- The top edge is a handle to resize it (the height is remembered in the browser); **Expand** makes it fill the window under the top bar.
- Opening the drawer with no shell opens one. Reloading the page lists the shells again and re-attaches to them.

## How it works

`runtime.Shells` (`internal/runtime/shells.go`) runs the user's shell (`$SHELL`) in a pseudo-terminal, in the project's folder, and keeps each one's `Output` (backlog + viewers), the same as an agent's terminal. `ShellsHandlers` (`shells_http.go`) mounts the routes described in `api/openapi.yaml`: `GET`/`POST /api/shells`, `DELETE /api/shells/{id}`, and the WebSocket `GET /api/shells/{id}/terminal`, which speaks the protocol of `TerminalHandler` (`terminal.go` runs both through `serveTerminal`).

A shell leaves the list when it exits or is closed (SIGHUP to its process group, SIGKILL after two seconds); the daemon closes them all when the project closes.

In the control room, `web/src/components/shell/ShellDrawer.tsx` is the drawer and `chat/TerminalView.tsx` the terminal, shared with the agents' (`onEnded` tells it that a shell does not come back, where an agent's terminal waits for the next session).

## Things to know

- The shell has the user's environment, not the project's variables (ADR-0020), and nothing it prints is read as data about an agent.
- It is arbitrary code execution as the user, behind the same guards as the rest of the API (loopback, cross-site guard, WebSocket origin). Real authentication comes first if the daemon ever listens beyond loopback.
