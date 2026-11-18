# The desktop app

Nobody Inc as an application: started like any app, it shows the projects page, opens one project per window, and stops a project when its window closes (ADR-0016). It is the launcher of `docs/projects.md` running inside a [Wails v3](https://wails.io) process, with native windows on the control room it serves.

## Using it

- **Build**: `task desktop:build` (macOS) exports the control room, builds the app and the binaries it ships with, and assembles `desktop/build/bin/Nobody Inc.app`. Move it to `/Applications` if you like. It is not signed: the first time, open it with right-click > Open.
- **Windows**: the first window shows the projects page. **Cmd/Ctrl+N** or **File > New Window** opens another; on Windows and Linux, launching the app again does too. On macOS, clicking the app's icon while it runs only brings it to the front.
- **One window, one project**, as in the browser (`docs/projects.md`): open a project from the projects page; Cmd/Ctrl-click (or the project menu) opens it in a new window. Opening a project that another window shows brings that window to the front instead.
- **Closing a window stops its project at once**, its agents with it; mail, tickets, features and the agents' sessions are kept (ADR-0015: they resume where they left off). **Closing the last window quits the app**, which stops every project first.
- **Logs**: `~/.nobody/desktop.log`, started afresh at each launch (the app's and its projects' daemons').

## How it works

`desktop/` is a Go module of its own (`github.com/qwackididuck/nobodyinc/desktop`, using the main module through a `replace`), so that Wails, cgo and the web view libraries never reach the main module, its CI or its linter.

```
Nobody Inc.app/Contents/
  Info.plist                 NSAllowsLocalNetworking: the windows load http://127.0.0.1
  MacOS/nobody-desktop       the app: launcher + windows
  MacOS/nobodyd              started as `nobodyd project ...` for each open project
  MacOS/nobody-hook          the agents' hooks
```

At start-up (`desktop/app.go`):

1. **PATH**: an app started from the Finder gets a minimal `PATH`, without `claude`. The app asks the user's login shell for its own (`$SHELL -ilc`, the value framed by markers so that what the shell prints is ignored; `desktop/path.go`). If `claude` is still missing, a native dialog says so; agents cannot start.
2. **The launcher** (`launcher.New`) listens on `127.0.0.1:7421` (another free port if taken: 7420 stays the development launcher's). The fixed port keeps the web view's storage (appearance, open panels) from one launch to the next. It serves the embedded control room (`internal/webui`) at `/`, its API and the projects' (`/p/<id>/api/...`) on the same origin.
3. **Windows** load `http://127.0.0.1:7421/?window=<id>&desktop=1`. The app chooses each window's id, which the page uses to hold its project (`web/src/lib/window.ts`), and `desktop=1` tells the page it runs in the app. Both are kept in the window's `sessionStorage`.

What the app adds to the launcher (`launcher.Options`, ADR-0016):

| Hook | When | What the app does |
|---|---|---|
| `Launcher.Release(window)` | A window's `WindowClosing` event | The window's project stops at once, without the grace period (which still covers a dropped connection: sleep, a crashed web view). |
| `OnHeld(project, holder)` | A window asks for a project another connected window holds | Brings the holder to the front and returns true: the launcher sends `held` with `focused`, and the window that asked goes back to the projects page. |
| `OpenWindow(project)` | `POST /api/windows` (only routed in the app) | Opens a window on the project, or on the projects page. The page calls it instead of `window.open` (a web view has no tabs). |
| `UI` | Any `GET` outside the API | Serves the control room (`webui.Handler`). `nobodyd` sets it too. |

Quitting (the last window, Cmd+Q, SIGTERM) runs Wails' `OnShutdown`: the launcher's server closes, then `Launcher.Close` stops every project daemon and waits for them.

## The control room as a static site

`task web:export` builds the control room with `NOBODY_WEB_EXPORT=1` (`output: "export"` in `web/next.config.ts`) and copies `web/out` into `internal/webui/dist`, embedded in `nobodyd` and the app (`go:embed`; only its README is versioned). Without it, both serve a page saying how to build it. In the export, WebSockets go to the page's own origin (`launcherWS` in `web/src/lib/api.ts`); with `next dev`, to `ws://127.0.0.1:7420`. CI exports it before building the release binaries, so `nobodyd` alone serves the control room.

The web view is the system's (WebKit on macOS): the control room must keep working in Safari's engine.

## Development

- `task desktop:run ARGS="-claude <built test/fakeclaude>"` builds and runs the app from `desktop/build/bin/` with free, deterministic agents (`task web:export` first for the control room). `NOBODY_HOME` points it to a throwaway home.
- `task desktop:check`: vet, golangci-lint (the main module's configuration) and tests of the desktop module. `task check` does not cover it.
- The launcher's side is tested in `test/e2e/desktop_test.go` (control room served, windows opened, focus, release).
- Not done yet: signing and notarization, an icon, Windows and Linux packages, building the app in CI, restoring the last session's windows.
