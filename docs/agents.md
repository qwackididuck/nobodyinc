# Agents: templates, recruitment and configuration

An **agent** is a member of a team that runs as a Claude Code session. A **template** is a job description in the catalog: recruiting an agent copies a template into a team under a name, then starts it. Decisions and rationale: ADR-0004.

## Where things live

```
<nobody binary>                           built-in templates (internal/catalog/builtin/)
~/.nobody/
  catalog/<template>.md                   your templates; same name as a built-in one = replaces it
  projects/<id>/nobody.db                 runtime state of one project on this machine (never shared)
<project>/.nobody/                        committed with the project
  org.yaml                                teams and channels
  mcp.yaml                                MCP servers agents may be given
  teams/<team>/<agent>.md                 recruited agents
```

The project directory is the one containing `.nobody/`; agents work in it, or in their team's worktree when a feature is active (`docs/workspaces.md`). The daemon reads `.nobody/` from the main checkout only.

## Adding a template

Write a markdown file in `~/.nobody/catalog/`. The catalog is read again on every request: no restart is needed. The file name (without `.md`) is the template's name.

```markdown
---
description: Writes Go code with table-driven tests   # shown in the catalog
version: 1                                            # bump when you change it
role: Go developer
model: sonnet
permissions:
  profile: allowlist
  allow: ["Bash(go test *)"]
tools: [Read, Edit, Write, Bash, Grep, Glob]
mcp: [gitlab]
skills: []
---
You implement what the specs describe, with unit tests.
When something is unclear, ask the specs team before guessing.
```

To ship a template with Nobody Inc, put the same file in `internal/catalog/builtin/` and rebuild.

## Agent file format

A recruited agent has the same fields, plus the ones that only make sense in a team:

| Field | Meaning | Launch |
|---|---|---|
| `role` | One-line description, shown to the agent and its colleagues. | system prompt |
| `model` | Claude model (`sonnet`, `opus`, `haiku`, a full id); empty = Claude Code's default. | `--model` |
| `permissions.profile` | `default`, `allowlist` or `auto` (see below). | `--permission-mode` |
| `permissions.allow`, `permissions.deny` | Claude Code permission rules added to the profile. | generated `--settings` |
| `tools` | Tools the agent may use without asking, added to the office tools. | `--allowedTools` |
| `mcp` | Names of servers from `.nobody/mcp.yaml`. | `--mcp-config` + `--strict-mcp-config` |
| `skills` | Skills made available to the agent. | not applied yet (ADR-0004) |
| `from` | `<template>@<version>` it was recruited from. Information only: template updates are never applied. | — |
| `on_leave` | `true` while the agent is on leave. | not started |
| body | The agent's instructions. | appended to the system prompt, after the office protocol |

The agent's name is the file name, its team the directory name: `teams/dev/dev1.md` is `dev1@dev`.

### Permission profiles

| Profile | Behaviour |
|---|---|
| `default` | Every sensitive action goes to the approval queue (`docs/approvals.md`). |
| `allowlist` | Claude Code's `acceptEdits` mode (editing files in the working directory passes), plus git (except `push`) and the usual test commands (`go test`, `npm test`, `pnpm test`, `pytest`, `cargo test`...); the rest goes to the approval queue. |
| `auto` | Claude Code's classifier decides. |

Whatever the profile, agents get the deny rules `Edit(./.nobody/**)` and `Write(./.nobody/**)`, and a delivery touching `.nobody/` is refused. The exact launch flags are built in `internal/runtime/command.go` and `permissions.go`.

## `org.yaml`

```yaml
teams:
  - id: specs
    goal: Turn the requirement into clear technical and functional specs.
  - id: dev
    goal: Implement the specs with unit tests.
channels:            # one-way: dev may start conversations with specs
  - from: dev
    to: specs
```

A team exists when it is declared here; its members are the files in `teams/<team>/`.

## `mcp.yaml`

```yaml
servers:
  gitlab:
    type: stdio
    command: gitlab-mcp
    env:
      GITLAB_TOKEN: ${GITLAB_TOKEN}   # never the value itself: this file is committed
  ragflow:
    type: http
    url: http://localhost:9380/mcp
```

The name `nobody` is reserved for the office server.

## Lifecycle

| Action | Effect |
|---|---|
| **Recruit** | Copies a template into `teams/<team>/<name>.md` with `from`, then starts the agent. |
| **Create** | Writes an agent file directly (no template), then starts it. Or writes a template to `~/.nobody/catalog/`. |
| **Edit** | Rewrites the agent file. The agent restarts with `--resume` and its new configuration once idle; "restart from scratch" starts a fresh session. Never touches the template. |
| **Leave** | Sets `on_leave: true`. Once idle the agent stops; its file and session are kept. Its ticket in progress goes back to the team queue, it is skipped by team addressing (`@dev`) and queue notifications, and direct mail waits in its mailbox; senders get a notice from `nobody`. **Return** clears the flag and resumes its session. |
| **Dismiss** | Once idle, the agent stops and its file is deleted. Its ticket in progress goes back to the team queue; undelivered mail is returned to the senders with a notice. History (messages, tickets, commits) is kept. |

All of these go through the daemon's API (`api/openapi.yaml`) and take effect without restarting the daemon.
