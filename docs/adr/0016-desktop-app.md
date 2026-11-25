# ADR-0016 — A desktop app: the launcher in a Wails process, one native window per project

- Status: accepted
- Date: 2026-11-18

## Context

Since ADR-0014, Nobody Inc is a launcher (`nobodyd`) and a control room that opens one project per browser window. Using it means two terminals (`task dev:daemon`, `task dev:web`) and a browser: fine for development, not for daily use. The target has always been a desktop application: started like any app, it shows the projects page; it can open several windows, one project each; closing a window stops that project; closing the last window stops everything.

The human chose **Wails v3** (stable since 2026-10-26, Go 1.26+): a Go library driving the system's web view (WebKit on macOS, WebView2 on Windows, WebKitGTK on Linux), with several windows, single-instance support and native menus. The launcher being Go, it can run inside the app's process. The alternatives were Electron (Node, a bundled Chromium, a child `nobodyd`) and Tauri (Rust).

Facts checked in Wails v3.0.0's source:

- a window may load any URL (`WebviewWindowOptions.URL` is resolved against the asset server's base URL, so an absolute `http://` URL is kept);
- `SingleInstanceOptions.OnSecondInstanceLaunch` runs in the first instance when the app is launched again (Windows, Linux; on macOS, launching a running app only activates it);
- a window's `WindowClosing` event can be hooked; `Mac.ApplicationShouldTerminateAfterLastWindowClosed` quits on macOS, other platforms quit on their own or through `App.Quit`; `OnShutdown` runs before the process ends;
- there is no Dock menu; application menus support roles (App, Edit, Window) and accelerators;
- macOS App Transport Security needs `NSAllowsLocalNetworking` for a web view to load `http://127.0.0.1`.

## Decision

### 1. The launcher serves the control room

The control room is exported as a static site (`output: "export"`, `task web:export`; it already uses no server feature of Next.js) and embedded in the Go binaries (`internal/webui`, `go:embed`). The launcher serves it at `/`, next to its API: the page, `/api/...` and `/p/<id>/api/...` share one origin, so REST, event streams and WebSockets need no proxy and no extra allowed origin. In the export, WebSockets go to the page's own origin.

This settles the open decision "front in production": a static export served by Go. `nobodyd` serves it too: the launcher alone is enough to use Nobody Inc in a browser (`http://127.0.0.1:7420`). Without an export (a fresh clone), the launcher serves a page saying how to build it. Development is unchanged: `next dev` on :3000 with its proxy.

### 2. The app: `nobody-desktop`, the launcher in its process

A separate Go module, `desktop/` (`github.com/qwackididuck/nobodyinc/desktop`, `replace` to the main module), so that Wails, cgo and the web view libraries never reach the main module, its CI or its linter. Its `main`:

- builds the launcher (`launcher.New`) and serves it on `127.0.0.1:7421` (another port if taken): a fixed port keeps the web view's storage (appearance, open panels) from one launch to the next, and does not clash with a development launcher on 7420;
- runs project daemons with the `nobodyd` and `nobody-hook` binaries shipped next to it (in the `.app`, `Contents/MacOS/`);
- opens windows on `http://127.0.0.1:<port>/?window=<id>&desktop=1`: the app chooses each window's id, so it knows which window holds which project;
- quits when its last window is closed; `OnShutdown` closes the launcher, which stops every project daemon and waits for them.

### 3. Windows

- **The first window** shows the projects page.
- **A new window**: `Cmd/Ctrl+N` or File > New Window; on Windows and Linux, launching the app again (single instance: the second process hands over and exits). On macOS, clicking the app's icon while it runs brings it to the front, as any Mac app (decided with the human); there is no Dock menu.
- **Closing a window releases its project at once** (`Launcher.Release(window)`, from the window's `WindowClosing` hook): no grace period, which stays for dropped connections (sleep, a crashed web view).
- **Opening a project held by another window** brings that window to the front instead of the "open in another window" screen: the launcher asks the app (`Options.OnHeld`), which focuses the window holding it; the window that asked goes back to the projects page.
- **"Open in a new window"** (Cmd/Ctrl-click a project, the project menu) asks the launcher to open one (`POST /api/windows`, served only when the app gives `Options.OpenWindow`), since a web view has no browser tabs. The page knows it runs in the app from `desktop=1`, kept in `sessionStorage` with its window id.
- Menus: App (About, Quit), File (New Window, Close Window), Edit (needed for copy and paste in a web view on macOS), Window.
- The control room is served at `/` for `GET` and `HEAD` only (a pattern without a method: `GET /` would conflict with the proxy's `/p/{project}/`).
- For development, `-claude <path>` replaces the `claude` found in the login shell's `PATH` (e.g. `test/fakeclaude`), and `NOBODY_HOME` a throwaway home, as for `nobodyd`. Logs go to `~/.nobody/desktop.log`.

### 4. The environment of an app started from the Finder

An app started from the Finder or the Dock does not get the shell's environment, so `claude` (often in `~/.local/bin` or Homebrew's directory) would not be found. At start-up the app reads the `PATH` of the user's login shell (`$SHELL -ilc`, between markers so that what the shell prints is ignored) and uses it for the launcher and every project daemon. If `claude` is still not found, the app shows a native error and quits.

**Amended on 2026-11-19, after the first real use: the whole environment, not only `PATH`.** Agents started from the app answered "Not logged in", and the MCP servers whose configuration refers to a variable got nothing: the app and its agents had none of the six `ANTHROPIC_*` variables of the user's shell, and Claude Code expands `${VARIABLE}` in MCP configurations from the daemon's environment (ADR-0010). The app now takes the login shell's whole environment (`env -0`), as the launcher started from a terminal already has it; the user chose this over an allowlist of authentication variables. The markers of a parent Claude Code session are left out (`runtime.CleanEnvironment`, ADR-0003 is unchanged: agents are still isolated from a parent session), and so are the variables that describe the process or the terminal (`PWD`, `TERM`, `TMPDIR`...). Consequence: every secret exported by the user's shell reaches the app, the project daemons and the agents, exactly as it does for a `claude` started in that shell; values are never logged.

### 5. Packaging

`task desktop:build` exports the web, builds the three binaries and assembles `Nobody Inc.app` (`Info.plist` with `NSAllowsLocalNetworking`, the binaries in `Contents/MacOS/`, the icon in `Contents/Resources/`), signed ad hoc. The Wails CLI is not needed: the app has no bindings.

- **The icon** is the control room's brand mark, drawn by a small Go program (`desktop/cmd/icongen`) from the same grid, so that it can be redrawn exactly; its outputs are versioned.
- **Releases** (added on 2026-11-19, the human's request): a merge into `main` builds the app on a macOS runner, as universal binaries (arm64 and amd64, cgo cross-compiling with clang), packages it as a DMG and attaches it to the GitHub release next to `nobodyd`.
- **Signing**: ad hoc only. Without a Developer ID and notarization, a downloaded app needs "Open Anyway" once per version; accepted for now (`docs/desktop.md`, "Signing").
- macOS first; Windows and Linux builds come later.

## Not decided here

- Developer ID signing, notarization, auto-update (Wails has an updater).
- Windows and Linux packages.
- Restoring the windows of the last session at start-up.

## Consequences

- One app to start; no terminal, no browser.
- The main module and its CI do not change; the desktop module has its own `go.mod`, lint and build tasks.
- `nobodyd` gains the control room: a release binary is usable on its own in a browser, once CI exports the web before building.
- The web view is the system's: WebKit on macOS, so the control room must keep working in Safari's engine.
