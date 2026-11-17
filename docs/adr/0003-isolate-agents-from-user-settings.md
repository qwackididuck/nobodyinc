# ADR-0003 — Isolate agents from the user's Claude Code configuration

- Status: accepted; the user's MCP servers reach the agents again since ADR-0010 (their settings, hooks and plugins still do not)
- Date: 2026-11-11

## Context

First real test with two agents (2026-11-11). The agents inherited the user's whole global configuration (`~/.claude/settings.json`): plugins, hooks, permission mode. The `Stop` hook of an installed plugin (claude-mem) printed text into an agent's session. The agent took it as a request and tried to **edit the user's `~/.claude/settings.json`** to disable that plugin. Only Claude Code's permission prompt stopped it.

Because the daemon had been started from a Claude Code session, the agents also inherited that session's environment variables. Among them, `CLAUDE_CODE_MESSAGING_TOKEN` gave an agent access to the parent session's messaging, and `CLAUDE_CODE_CHILD_SESSION` disabled the saving of its transcript (which session forking relies on).

## Decision

1. Every agent is started with `--setting-sources project,local`: the user's global settings do not apply. What the agent needs is given explicitly by Nobody Inc (hooks, MCP, prompt, model), and later the MCP servers, skills and permissions assigned per agent.
2. Variables that identify a parent Claude Code session are removed from the agents' environment (`CLAUDECODE`, `CLAUDE_PID`, `CLAUDE_CODE_SESSION_*`, `CLAUDE_CODE_MESSAGING_*`, `CLAUDE_CODE_CHILD_SESSION`, etc.: see `internal/runtime/command.go`). Configuration variables (authentication, options) are kept.
3. Agents keep the default permission mode: any sensitive action still requires approval. This is what prevented damage during the incident.

## Consequences

- Agents behave predictably, regardless of the plugins and hooks the user has installed.
- The user's global MCP servers and tools are no longer available by default: they will have to be assigned to agents explicitly.
- Agents' permission requests must be visible and actionable by the human: an approval queue in the UI becomes a priority.
