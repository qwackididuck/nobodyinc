# ADR-0010 — MCP servers come from Claude Code's own scopes

- Status: accepted
- Date: 2026-11-17
- Replaces: the MCP registry `.nobody/mcp.yaml` and the per-agent `mcp:` list of ADR-0004
- Revisits, in part: ADR-0003 (the user's MCP servers reach the agents again; their settings, hooks and plugins still do not)

## Context

Agents get their MCP servers from Nobody Inc alone: a registry in `.nobody/mcp.yaml`, and an `mcp:` list in each agent's file that picks servers from it. They are started with `--strict-mcp-config`, which makes Claude Code ignore every other source.

So a project that already declares its servers for Claude Code, in `.mcp.json`, has to declare them a second time for Nobody Inc. The user's own servers, added once with `claude mcp add`, are not available either. Everything else of the project's Claude Code configuration already reaches the agents (`CLAUDE.md`, or `AGENTS.md` when there is no `CLAUDE.md`, and `.claude/skills`, `commands`, `agents`, `settings.json`), through `--setting-sources project,local`.

Claude Code has three MCP scopes:

| Scope | Meaning | Stored in |
|---|---|---|
| project | for everyone working on the project, committed | `.mcp.json` at the project's root |
| local | for this user, on this project only | `~/.claude.json`, under `projects.<project path>.mcpServers` |
| user | for this user, on all their projects | `~/.claude.json`, under `mcpServers` |

Checked by `test/contract/project_test.go` (Claude Code 2.1.326): with the agents' settings sources, `.mcp.json` is loaded only without `--strict-mcp-config`, and the user's servers are never loaded, strict or not. Loading them would take the `user` settings source, which also brings back the user's settings, hooks and plugins: what ADR-0003 cut off after an agent tried to edit the user's settings to silence a plugin's hook.

## Decision

1. **Three sources, no registry of our own.** An agent's MCP servers are those of the project (`.mcp.json`), of the user on this project (local scope) and of the user (user scope), read by the daemon from Claude Code's own files. `.nobody/mcp.yaml` is removed, without migration (nothing is in production). `~/.claude.json` is found as Claude Code finds it (`$CLAUDE_CONFIG_DIR`, else the home directory); the local scope is looked up under the project's main checkout, whatever worktree the agent works in.
2. **Cumulative; the project wins.** On a name conflict: project, then local, then user. What the project declares for everyone is what its agents use. A server named `nobody`, the office server, is ignored with a warning, whatever its source.
3. **Every agent gets every server.** The `mcp:` list of the agent file is removed. An agent's file may instead exclude servers or single tools (`mcp_exclude`, e.g. `[gitlab, ragflow/delete_document]`), applied with `--disallowedTools mcp__<server>` or `mcp__<server>__<tool>`. Editing an agent lists the tools of each server, which means starting the server (with its variables) to ask it (`tools/list`). An exclusion that names a server absent from this machine is ignored.
4. **Nobody Inc still builds each agent's MCP configuration.** Agents keep `--strict-mcp-config`. The daemon merges the three sources, adds the office server, and writes the result to a file only the user can read, in the project's data directory (`~/.nobody/projects/<id>/`), passed with `--mcp-config <file>` instead of inline JSON. The user's files may hold secrets in clear, which must not show on a command line (`ps`). Agents never see Claude Code's MCP approval prompt.
5. **Adding a server from Nobody Inc asks where it goes**, with three answers, and writes only after that explicit answer:
   - *for the whole project* → `.mcp.json` (committed: everyone, Claude Code included, gets it);
   - *for me, on this project* → local scope;
   - *for me, on all my projects* → user scope.
   The write goes through `claude mcp add --scope project|local|user`, so that Claude Code keeps owning its files' format and the servers and keys Nobody Inc does not know.
6. **No secret is ever written.** Nobody Inc writes environment variables only (`${GITLAB_TOKEN}`), expanded by Claude Code when it starts the server. When a server needs credentials, Nobody Inc names the variables to set, in the environment the daemon is started from; the values are managed outside the tool. Values already written in clear in a file are left as they are, and never returned by the API, shown in the UI or logged: only variable names, or a masked value.
7. **A change reaches running agents once idle.** A server added, removed or changed in any source, or a new exclusion, restarts the agents concerned once idle, resuming their session (MCP servers are launch flags, followed on `--resume`). Files edited outside Nobody Inc are picked up at the next change made through it, or when the daemon restarts, as for agent files.

## Not decided here

- Suggesting servers to install (a curated list), and the director asking for one (`request_mcp` through the approval queue).
- Health of the servers, and per-team or per-run assignment.

## Consequences

- A project's MCP servers are declared once, for Claude Code and for its agents alike; the user's own servers follow them into every project.
- Two developers on the same project may give their agents different tools (their own local and user servers). The committed part (`.mcp.json`, agent files and their exclusions) stays the same for both.
- ADR-0003's isolation narrows to settings, hooks and plugins: the user's MCP servers, and any secret their configuration holds in clear, now reach the agents. A server is a tool the agent calls, not code that writes into its session, which is what the incident was about.
- Nobody Inc reads two files it does not own, `.mcp.json` and `~/.claude.json`, and must follow their format (server types, `env`, `headers`, `${VAR:-default}`); the contract test guards how Claude Code loads them.
- `.nobody/mcp.yaml`, `org.MCPServer` and the `mcp:` field go; the MCP part of the Settings tab and `GET /api/mcp` show the merged servers with their source.
