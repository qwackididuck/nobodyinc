# Architecture

Nobody Inc runs an organisation of Claude Code agents. This page describes the pieces of the walking skeleton and how a message travels. Decisions are recorded in `adr/`.

## Overview

```
            Browser: Next.js UI (web/)
            office · chat · terminal · kanban · staff
                 │ REST + SSE events (/api)   │ terminal WebSocket
                 ▼                            ▼
┌──────────────────────────── nobodyd (cmd/nobodyd) ─────────────────────────────┐
│ server     /api/* (UI), /internal/* (hooks)          officemcp  /mcp/{address} │
│ messaging  sending rules, threads, hop cap           runtime    PTYs, statuses,│
│ org        teams, agents, block lists (<project>/.nobody/)      wake-up, terms │
│ store      SQLite: messages, tickets, features (versioned migrations)          │
└───────────────┬───────────────────────────────▲──────────────────▲─────────────┘
                │ starts in a PTY               │ hooks (HTTP)     │ MCP (HTTP)
                ▼                               │                  │
     claude (one per agent) ── Stop / PostToolUse ─► nobody-hook   │
                            └── tools send_message, list_contacts ─┘
```

| Package | Role |
|---|---|
| `internal/org` | Loads and validates the project's `.nobody/` (`agents.md`): teams, agents (`<agent>@<team>`), each team's block list (ADR-0009). |
| `internal/catalog` | Agent templates (built-in and `~/.nobody/catalog`) and the agent file format shared with recruits (`agents.md`). |
| `internal/messaging` | The post office: validates a draft, attaches it to its thread, applies the sending rules (`policy.go`), files it in the recipient's tray. |
| `internal/store` | SQLite (pure-Go driver), schema evolved by versioned migrations: messages (pending until delivered) and tickets. |
| `internal/work` | Tickets and features: team queues, claims, dependencies, deliverables, comments, and who may do what (`tickets.md`). |
| `internal/workspace` | The project's git repository: feature branches, team worktrees, and the merges that move work between teams (`workspaces.md`). |
| `internal/hook` | Decides what to hand an agent on a Claude Code hook, and formats the mail for the model. |
| `internal/officemcp` | Each agent's MCP server: `send_message`, `list_contacts` and the ticket tools. The address in the URL is the agent's identity. |
| `internal/approval` | The queue of agents' permission requests waiting for the human (`approvals.md`). |
| `internal/runtime` | Starts each agent (`claude` in a PTY), tracks its status and session, wakes it up when mail arrives, answers urgent questions to busy agents with a session fork, streams its terminal over a WebSocket. |
| `internal/server` | The HTTP API described in `api/openapi.yaml`. |
| `internal/daemon` | Wires all of the above together, turns ticket events into notices, picks the member of a team that gets team-addressed work; `cmd/nobodyd` adds flags and a listener. |
| `cmd/nobody-hook` | Relay run by Claude Code on hooks: forwards the event to the daemon, prints the answer, acknowledges. |

## Conversation rules

Flows between teams are open by default; a team may restrict them with a block list (ADR-0009). Applied by `messaging.Policy`, in this order:

1. the human, the director (`docs/god.md`) and the system address `nobody` (notices written by Nobody Inc itself) may write to anyone; nobody can write to `nobody`;
2. any agent may write to the human or the director (to answer or escalate);
3. any agent may answer a message it received (`in_reply_to`), whatever the blocks;
4. otherwise, any agent may start a conversation with any agent of any team, its own included, unless its team blocks the recipient's team (`block:` in `org.yaml`, `org.CanStart`). A block is one-way: `dev` blocking `qa` says nothing about `qa` writing to `dev`.

A message to `@<team>` goes to one member of that team (idle first), then follows the same rules: a blocked team stays blocked through its team address. A refused message comes back to the agent as an error (`messaging.ErrBlocked`) telling it to ask the human instead. Blocks never apply to tickets (`tickets.md`).

Two guards keep agents from talking forever:
- a message that expected no answer (`inform`, `agree`, `refuse`, `done`) cannot be answered by another such message: no thank-you notes, no acknowledgements of acknowledgements (the human is exempt);
- a conversation is capped at 10 exchanges; beyond that, it must be escalated to the human.

## The journey of a message

Example observed during the first real test: the human asks `dev1@dev` to consult the specs team.

1. **The human sends** (UI → `POST /api/messages`). The message is filed in `dev1`'s normal tray.
2. **Wake-up**: `dev1` is idle. The supervisor types a short prompt into its terminal, which starts a turn. Only keystrokes are ever written: the agent's screen is never read.
3. **Delivery**: submitting the prompt fires the `UserPromptSubmit` hook; `nobody-hook` asks the daemon what to say, and the daemon attaches the mail to the prompt. `nobody-hook` acknowledges, and the message is marked delivered.
4. **The agent acts**: `dev1` calls `send_message` (MCP) to send a `query` to `archi@specs`: flows are open, and team `dev` does not block `specs`.
5. Same cycle on `archi`'s side (wake-up, delivery, answer), then back to `dev1`, who finally answers the human.

Hook details, and how urgent questions to busy agents are answered by a fork of their session, are in `hooks.md`.

## Agent status

`stopped → starting → working ⇄ idle → exited`, plus `awaiting_approval` while a permission request waits for the human, derived from the hooks: a `Stop` with nothing to deliver means the agent really finished its turn (`idle`); any other event means it is working. At launch, a short onboarding turn brings the agent to `idle`, the state in which mail wakes it up.

## Agent isolation

Agents see neither the user's global settings nor the environment of a parent Claude Code session (ADR-0003). They keep the default permission mode: any sensitive action requires approval, which reaches the human through the approval queue of the UI (`approvals.md`).

## Testing

- Unit tests next to each package.
- `test/e2e`: whole scenarios (wake-up, delivery, MCP, refusals by a block) through the real daemon, `nobody-hook` and PTYs, with `test/fakeclaude` playing the agents by fixed rules. Free and deterministic, part of `task check`.
- `test/contract` (`task contract`): the Claude Code behaviours we rely on, against the real CLI.

## Changes pushed to the browser

The control room does not poll: `GET /api/events` streams every change as Server-Sent Events (agent statuses, messages sent and delivered, approvals, tickets, features, the organization, the catalog), and the UI reads the changed resource again through the REST API. See `docs/events.md` and ADR-0006.

## The terminal in the browser

Claude Code runs in a real PTY. The daemon keeps the last 256 KB of each agent's output and fans it out to connected browsers (`runtime/output.go`, `runtime/terminal.go`). On connect, the UI sends its terminal size, which makes Claude Code redraw its screen. xterm.js's automatic answers to terminal queries are not forwarded to the agent (`TerminalView.tsx`), so that they do not arrive as typed text.
