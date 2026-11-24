# ADR-0020 — Environment variables of a project

- Status: accepted
- Date: 2026-11-24
- Builds on: ADR-0003 (agents isolated from the user's environment), ADR-0010 (MCP servers refer to variables, never hold a secret), ADR-0014 (a state directory and a daemon per project)

## Context

An agent's process gets the daemon's environment, and the daemon gets the environment of whoever started it (the launcher, the desktop app, a shell). To give a project its own Claude key, or the token an MCP server refers to as `${GITLAB_TOKEN}` (ADR-0010), the user had to export it in the shell that starts Nobody Inc: that sets it for every project at once, and a desktop app started from the Dock does not see the shell at all.

## Decision

1. **A project has its own environment variables.** The agents' processes get them on top of the daemon's environment, and they replace a variable of the same name. They apply to every process Nobody Inc starts for the project: agents, the forks that answer urgent questions, the discovery of built-in skills, and the probing of MCP servers (`GET /api/mcp/{name}/tools`). They never reach the user's shell, the daemon's own environment, nor another project: the overlay is made on a copy, for the process about to be started.

2. **They live in the project's state directory**, `~/.nobody/projects/<id>/env.yaml`, readable by the user only (mode `0600`, written atomically). Never in `.nobody/`, which may be committed.

3. **A value is never read back.** The API lists names and takes values; the control room, the logs and the events never show one. To change a value, set it again. This follows the spirit of ADR-0010, which keeps secrets out of the files of Claude Code: here Nobody Inc is the one holding a secret, so it holds it where it holds the other private state, and says so. `.mcp.json` and `~/.claude.json` still never receive a secret from Nobody Inc.

4. **Some names are reserved**, because Nobody Inc sets them or removes them on purpose: `NOBODY_*`, `GIT_AUTHOR_*`, `GIT_COMMITTER_*` (the hooks and the agents' git identity), and the markers of a parent Claude Code session removed by ADR-0003 (`CLAUDECODE`, `CLAUDE_CODE_SESSION_*`, `CLAUDE_CODE_MESSAGING_*`…). They are refused when set, so that the isolation of ADR-0003 cannot be undone by a variable. A test keeps the two lists in line. The agent's own variables are added after the project's, as a second guard.

5. **Changing them restarts the agents.** A process keeps the environment it started with. Every agent restarts once idle, resuming its session, like when MCP servers change: no turn is interrupted.

6. **The state is not compared with the user's wishes about the key's source.** A Claude key set here replaces the user's for this project's agents; nothing else about authentication changes.

## Consequences

- One project can use another Claude account, or other tokens, than the next one, without touching a shell.
- `${VARIABLE}` in an MCP server is expanded by Claude Code from the agent's environment, so a variable set here is enough; the list of MCP servers shows it as set.
- A new place holds secrets in clear on disk, protected by file permissions only, like `~/.claude.json`. Encrypting it (keychain) is left for later.
- Not covered: variables per agent or per team, and variables for the user's other projects (every project has its own file).
