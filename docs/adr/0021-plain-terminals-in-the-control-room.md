# ADR-0021 — Plain terminals in the control room

- Status: accepted
- Date: 2026-11-24
- Builds on: ADR-0003 (agents isolated from the user's settings), ADR-0014 (a daemon per project), ADR-0020 (project environment variables)

## Context

The control room shows an agent's terminal, but to run a command in the project (a build, `git status`, a test the agents just touched) the human had to leave for another terminal window, find the project's folder and come back.

## Decision

1. **A shell is a pseudo-terminal running the user's own shell** (`$SHELL`, else `/bin/sh`) in the project's folder. No agent, no Claude Code, no hook: it is not part of the organization, and nothing it prints is read as data about an agent.

2. **It belongs to the project's daemon, not to the page.** The daemon keeps the shells until they are closed, they exit, or the project closes: reloading the control room lists them again (`GET /api/shells`) and attaches to each, getting the recent output back. Up to 8 shells per project.

3. **It speaks the agents' terminal protocol** (binary output frames, JSON `input` and `resize`), so the control room reuses one terminal component for both. The backlog and viewer fan-out (`runtime.Output`) are shared too.

4. **Environment: the user's, not the project's.** The shell gets the daemon's environment (the user's) with `TERM` and `COLORTERM` set. The project's variables (ADR-0020) are not added: that ADR keeps them out of the user's shell, and a shell must behave as the user's terminal would. It is not an agent, so ADR-0003's isolation does not apply either.

5. **Closing is a hangup**, like closing a terminal window: the shell's whole process group gets SIGHUP, then SIGKILL after two seconds.

6. **Same protection as the rest of the API** (loopback by default, the cross-site guard on `POST` and `DELETE`, the origin check on the WebSocket). A shell is arbitrary code execution as the user: anything allowed to call this API could already start an agent with permissions, but the day the daemon listens beyond loopback (VPS mode, `docs/architecture.md`), these routes need real authentication first.

## Consequences

- The control room gets a drawer with tabs of shells; the human no longer leaves the window to run a command.
- Not covered: choosing the working directory (a team's worktree), persisting shells across a project's restart, and sharing a shell between windows' lists (each window reads the list when it opens).
