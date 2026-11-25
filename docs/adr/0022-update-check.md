# ADR-0022 — Telling the human that a newer release exists

- Status: accepted
- Date: 2026-11-25
- Builds on: ADR-0014 (the launcher), ADR-0016 (the control room and the desktop app)

## Context

CI publishes a GitHub release (`vX.Y.Z`, archives and a macOS DMG) at every release-worthy merge to `main`. Nothing told the human a newer one existed, so a long-lived install stayed on its version until the human thought of looking.

## Decision

1. **The launcher asks GitHub, once, when it starts.** `GET https://api.github.com/repos/qwackididuck/nobodyinc/releases/latest` (drafts and prereleases excluded by GitHub), in the background, with a 5 s timeout: starting never waits for the network, and a failure (offline, rate limit, repository not public) is a log line and an `error` field, never a refusal to start.

2. **The request is anonymous.** A GET of a public URL with the generic `User-Agent: nobody-inc`. Nothing about the user, their projects or their agents is sent. This is the daemon's first outbound call to the internet; it is made by the launcher only, never by the project daemons, so the agents' isolation (ADR-0003) is untouched. It needs the repository to be public.

3. **On by default, switchable.** `check_updates: true` in `~/.nobody/settings.yaml`; `false` skips the check at start. "Check for updates" (the control room's menus, the desktop app's File menu) works either way.

4. **Only releases are compared.** A build whose version is not `vX.Y.Z` (`dev`, or `git describe` output such as `v1.2.3-4-gabc-dirty`) is never told to update. The comparison is a small strict parser in `internal/updates`, not a dependency.

5. **A banner, not an installer.** The control room shows a card with a link to the release page and a "Later" that hides that release (a newer one shows again). Nothing is downloaded or installed. The launcher keeps the last result in memory (`GET /api/update`; `POST /api/update/check` asks again). It is a plain GET at page load, because the launcher has no event stream (events are per project).

## Consequences

- Installing stays a manual step. Updating in place depends on the OS and on signing (`docs/desktop.md`, "Signing"); it is for a later ADR.
- The desktop app's menu shows a native dialog with the same result and opens the release page in the browser.
- A repository that is still private makes every check fail with a 404, reported in `error`.
