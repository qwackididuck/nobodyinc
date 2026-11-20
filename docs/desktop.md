# The desktop app

Nobody Inc as an application: started like any app, it shows the projects page, opens one project per window, and stops a project when its window closes (ADR-0016). It is the launcher of `docs/projects.md` running inside a [Wails v3](https://wails.io) process, with native windows on the control room it serves.

## Using it

- **Download**: every release (a merge into `main`, see `.github/workflows/ci.yml`) carries `NobodyInc_<version>_macos_universal.dmg`, for Apple silicon and Intel Macs (macOS 13+). Open it and drag Nobody Inc to Applications.
- **First launch**: the app has no Apple signature (see "Signing" below), so macOS refuses to open it the first time ("Apple could not verify…"). Open System Settings > Privacy & Security, then **Open Anyway** next to the message about Nobody Inc; or, in a terminal, `xattr -dr com.apple.quarantine "/Applications/Nobody Inc.app"`. Once is enough, until the next version.
- **Build it yourself**: `task desktop:build` (macOS) exports the control room, builds the app and the binaries it ships with, and assembles `desktop/build/bin/Nobody Inc.app` (`ARCHS="arm64 amd64"` for a universal app); `task desktop:dmg` packages it. A build of your own is not quarantined: it opens directly.
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
  Resources/icons.icns       the icon
```

Each binary is universal (`lipo` of an arm64 and an amd64 build; cgo cross-compiles with the system's clang) when built with `ARCHS="arm64 amd64"`, as CI does.

**The icon** is the control room's brand mark (the dotted outline of a person in the top bar, `BrandMark`), in its accent color on a rounded square of its background color. `desktop/cmd/icongen` draws it at every size from the same 13×13 grid (`task desktop:icon`) into `desktop/build/appicon.png` (the About box, embedded) and `desktop/build/darwin/icons.icns` (the bundle); both are versioned.

At start-up (`desktop/app.go`):

1. **Environment**: an app started from the Finder gets a minimal environment: a `PATH` without `claude`, none of your credentials (`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_USE_VERTEX`…), none of the variables your MCP servers refer to (`${GITLAB_TOKEN}`). The app asks the user's login shell for its whole environment (`$SHELL -ilc 'env -0'`, between markers so that what the shell prints is ignored; `desktop/loginenv.go`) and takes it as its own, so that the launcher, every project daemon and every agent inherit it, as they do when the launcher is started from a terminal. Left out: the markers of a parent Claude Code session (`runtime.CleanEnvironment`), and what describes the process or the terminal rather than you (`PWD`, `TERM`, `TMPDIR`, `XPC_*`, `ITERM_*`…), which the app keeps. Values are never logged, only their number (`~/.nobody/desktop.log`). If the shell cannot be read, the app keeps its own environment and says so in the log. If `claude` is still missing, a native dialog says so; agents cannot start. It needs `env -0` (macOS 12 and later).
2. **The launcher** (`launcher.New`) listens on `127.0.0.1:7421` (another free port if taken: 7420 stays the development launcher's). The fixed port keeps the web view's storage (appearance, open panels) from one launch to the next. It serves the embedded control room (`internal/webui`) at `/`, its API and the projects' (`/p/<id>/api/...`) on the same origin.
3. **Windows** load `http://127.0.0.1:7421/?window=<id>&desktop=1`. The app chooses each window's id, which the page uses to hold its project (`web/src/lib/window.ts`), and `desktop=1` tells the page it runs in the app. Both are kept in the window's `sessionStorage`.

What the app adds to the launcher (`launcher.Options`, ADR-0016):

| Hook | When | What the app does |
|---|---|---|
| `Launcher.Release(window)` | A window's `WindowClosing` event | The window's project stops at once, without the grace period (which still covers a dropped connection: sleep, a crashed web view). |
| `OnHeld(project, holder)` | A window asks for a project another connected window holds | Brings the holder to the front and returns true: the launcher sends `held` with `focused`, and the window that asked goes back to the projects page. |
| `OpenWindow(project)` | `POST /api/windows` (only routed in the app) | Opens a window on the project, or on the projects page. The page calls it instead of `window.open` (a web view has no tabs). |
| `UI` | Any `GET` outside the API | Serves the control room (`webui.Handler`). `nobodyd` sets it too. |

### Quitting

Stopping the project daemons takes a moment, and Wails runs its `OnShutdown` hook **on the app's main thread, before it closes the windows**: stopping there would leave the window up and frozen, with no sign of what is happening. So the app does it itself, first (`desktop/quit.go`):

1. A request to quit (the last window closed, Cmd+Q, the Quit menu) reaches `ShouldQuit` (`desktop.shouldQuit`). The first one answers *not yet* and starts stopping in the background; the ones during it answer *not yet* too; once everything is stopped it answers yes.
2. Every window closes at once (each releases its project), and no new window opens meanwhile.
3. The launcher's server closes and `Launcher.Close` stops every project daemon, while the interface stays alive.
4. If that takes more than 0.4 seconds, a small frameless window, over everything, says **"Closing Nobody Inc… Stopping your agents. Their sessions are kept."** (`desktop/closing.go`: a page of its own, with no server and no script, since the launcher's has closed). Stopping usually takes milliseconds: then no window flashes by.
5. The app quits for good (`App.Quit`).

`OnShutdown` still stops everything too (`Launcher.Close` is safe to call twice): it is for the ways of quitting that do not ask, a signal. How long stopping took is in `desktop.log` ("quitting: the project daemons stopped", "project daemons stopped"), with a warning about a daemon that took more than a second.

A project daemon stops on SIGTERM: its agents are killed (`Supervisor.StopAll`, which does not wait) and its HTTP server shuts down gracefully, waiting up to 10 seconds for the requests still open. An agent waiting for your approval holds one open (its `PermissionRequest` hook, `docs/approvals.md`), so the daemon abandons the pending approvals when it is told to stop (`approval.Queue.Close`): the agents fall back to their terminal, as after a restart, and quitting no longer waits for them (`test/e2e/quit_test.go`).

## The control room as a static site

`task web:export` builds the control room with `NOBODY_WEB_EXPORT=1` (`output: "export"` in `web/next.config.ts`) and copies `web/out` into `internal/webui/dist`, embedded in `nobodyd` and the app (`go:embed`; only its README is versioned). Without it, both serve a page saying how to build it. In the export, WebSockets go to the page's own origin (`launcherWS` in `web/src/lib/api.ts`); with `next dev`, to `ws://127.0.0.1:7420`. CI exports it before building the release binaries, so `nobodyd` alone serves the control room.

The web view is the system's (WebKit on macOS): the control room must keep working in Safari's engine.

## Signing

The bundle is signed **ad hoc** (`codesign --sign -`, no identity): enough for the system to run it (Apple silicon requires a signature) and to check its files are intact, but it says nothing about who made it. A downloaded app is quarantined by the browser, and Gatekeeper only lets a quarantined app open without a warning if it is:

1. **signed with a Developer ID** certificate (Apple Developer Program, 99 USD a year), which names its author, and
2. **notarized**: uploaded to Apple, scanned, and stamped (`xcrun notarytool`, then `xcrun stapler`).

What that would bring: no "Open Anyway" step for whoever downloads it; a stable identity, so that macOS keeps the permissions granted to the app (folders, notifications) from one version to the next instead of asking again; and, later, automatic updates (Wails' updater checks signatures). For a tool used by its author and a few people who build or trust it, the ad-hoc signature is enough; signing is worth it once the app is handed to people who should not have to bypass Gatekeeper. It would add to the CI's `desktop` job: the certificate and an App Store Connect API key as secrets, `codesign --options runtime --timestamp` with the identity, then notarization.

On Windows the equivalent is Authenticode (a code-signing certificate), without which SmartScreen warns; Linux has none.

## Development

- `task desktop:run ARGS="-claude <built test/fakeclaude>"` builds and runs the app from `desktop/build/bin/` with free, deterministic agents (`task web:export` first for the control room). `NOBODY_HOME` points it to a throwaway home.
- `task desktop:check`: vet, golangci-lint (the main module's configuration) and tests of the desktop module. `task check` does not cover it.
- The launcher's side is tested in `test/e2e/desktop_test.go` (control room served, windows opened, focus, release).
- **Release**: on `main`, the CI's `desktop` job (a macOS runner) runs the same tasks (`task desktop:build ARCHS="arm64 amd64" VERSION=<tag>`, `task desktop:dmg`) and attaches the DMG to the release.
- Not done yet: Developer ID signing and notarization, Windows and Linux packages, restoring the last session's windows.
