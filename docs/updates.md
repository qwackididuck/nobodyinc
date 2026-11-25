# Updates

Nobody Inc tells you when a newer release exists (ADR-0022). It never downloads or installs anything: it links to the release page.

## What happens

When the launcher (`nobodyd`, or the desktop app) starts, it asks GitHub for the repository's latest release, in the background. If that release is newer than the running build, the control room shows a card in the bottom-left corner of the window, on the projects page and in a project:

> **v1.4.0** is available (this is v1.3.2). [Release notes] [Later]

- **Release notes** opens the release page, where the archives and the macOS DMG are.
- **Later** hides the card for that release. A newer release shows it again.

## Checking by hand

- Control room: "Check for updates" at the right of the projects page's top bar, and in the project menu (the project's name in the top bar). The answer is a card too: the new release, "up to date", or why the check failed. A check made on purpose shows the banner again even after "Later".
- Desktop app: File > Check for Updates…, a native dialog with a button to open the release page.

## What is sent, and what is compared

One anonymous `GET` to `api.github.com` with `User-Agent: nobody-inc`: nothing about you, your projects or your agents. Only versions of the form `vX.Y.Z` are compared; a development build (`dev`, or `git describe` output like `v1.2.3-4-gabc-dirty`) is never told to update.

The check needs the repository to be public. While it is not, or when you are offline, it fails quietly (a debug log line; "Check for updates" says why).

## Turning the check at start off

In `~/.nobody/settings.yaml`:

```yaml
check_updates: false
```

Restart the launcher after editing the file. "Check for updates" keeps working.

## Where it lives

| What | Where |
|---|---|
| The check, the version comparison | `internal/updates` |
| The routes `GET /api/update`, `POST /api/update/check`; the check at start | `internal/launcher/update.go` |
| The setting | `internal/settings` (`check_updates`) |
| The card and the shared state | `web/src/components/shell/UpdateNotice.tsx`, `web/src/lib/update.ts` |
| The desktop menu | `desktop/update.go` |

Tests: `internal/updates` (versions, a fake GitHub), and `test/e2e` (`TestLauncherReportsNewRelease`, `TestLauncherUpdateCheckCanBeOff`). The e2e launcher turns the check at start off unless a test gives it a fake GitHub: tests never reach the real one.
