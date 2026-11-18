# Delivering mail to agents: the hooks

A Nobody Inc agent is an ordinary Claude Code session. To hand it its mail without ever reading or simulating its screen, we rely on Claude Code **hooks**: commands Claude Code runs at specific moments of an agent's life, sending them an event as JSON on stdin and reading their JSON answer on stdout.

## Who does what

| Piece | Role |
|---|---|
| `nobody-hook` (`cmd/nobody-hook`) | The binary Claude Code runs. A **plain relay**: it forwards the event to the daemon, prints the answer, then acknowledges the delivery. No business logic. |
| `nobodyd` daemon (`internal/server/hooks.go`) | Owns the mailboxes. Receives the event, decides what to say, updates the agent's status. |
| `internal/hook` | The decision itself (`hook.Handle`) and the formatting of the mail for the model. Unit tested. |
| `internal/store` | The messages, in SQLite. A message stays pending until it is marked delivered. |

## When mail is handed over

| Hook | Mail handed over | Typical case |
|---|---|---|
| `UserPromptSubmit` | All waiting mail, urgent first, **attached to the prompt** as extra context | The daemon wakes an idle agent up by typing a short prompt; the agent reads its mail in that same turn. Also fires when the human types into the agent's terminal. |
| `PostToolUse` | **Urgent** mail only, as extra context, without stopping the agent | Urgent mail arriving while the agent works. |
| `Stop` | All waiting mail; the end of the turn is refused and the mail is given as the reason | Mail that arrived during a turn: the agent deals with it before finishing. |

Two more hooks are not about mail. `PermissionRequest` sends an agent's permission requests to the human (`docs/approvals.md`). `SessionStart` hands the agent the skills it preloads (its file's `skills:`, `docs/agents.md` "Skills"): at every session start, new, resumed (`source: resume`) or compacted, the daemon answers with each skill's instructions as extra context, read in the directory the agent works in (`skills.Preload`, `hook.SessionStartResponse`). When the session was resumed because the project opened again (ADR-0015, `docs/agents.md` "Sessions across openings"), the first `SessionStart` also hands over the current organization, and a session resumed without a prompt becomes `idle` (`Supervisor.SessionStarted`). It also tells the daemon the agent's session ID, which the daemon remembers. Nothing is acknowledged.

So the two trays behave like this: `normal` mail is never handed over mid-work; `urgent` mail reaches the agent right after its next tool call.

**Budgets** (`docs/budgets.md`, ADR-0013) come first at `Stop`: an agent a budget at its threshold holds gets no mail. The first agent to stop on a request awaiting its justification has the end of its turn refused once, with a reason asking it to call `budget_request`; any other held agent ends its turn on budget hold (status `budget_hold`, not idle). The same relay is also every agent's **statusLine**: `nobody-hook statusline` posts the JSON Claude Code hands it, with the session's running cost, to `POST /internal/agents/{address}/statusline`, and prints nothing.

> Claude Code labels a blocking `Stop` hook "Stop hook error" in its UI, even though the hook works as documented (known issue, e.g. anthropics/claude-code#84385). Waking idle agents through `UserPromptSubmit` avoids it in the most common case.

## Urgent questions to a busy agent

An urgent `query` sent to an agent that is in the middle of a turn does not wait for its next tool call: the daemon forks the agent's Claude Code session (`claude -p --resume <session> --fork-session --tools ""`, see `internal/runtime/fork.go`). The fork knows everything the agent knows, has no tools, and leaves the original session untouched.

- The question leaves the agent's tray while the fork thinks, so the agent does not also get it from its hooks.
- If the fork answers, the asker gets the answer at once (signed by the agent, marked as a quick answer), and the agent gets a notice from `nobody` with the question and the answer, to send a correction if needed.
- If the fork answers `ESCALATE` (the point is not settled in the conversation) or fails, the question goes back to the tray and follows the usual path.
- An idle agent is not forked: it is woken up and reads the question right away.
- The fork's cost, read from its `-p` output, is charged to the asker, not to the agent forked (`docs/budgets.md`).

The agent's session ID is learnt from the `session_id` field of its hook events.

## Flow

```
Agent (claude)            nobody-hook                      nobodyd daemon
      │                        │                                 │
      │ wake-up prompt typed   │                                 │
      ├─ UserPromptSubmit ────►│── POST /internal/agents/A/hook ►│ any mail?
      │◄─ additionalContext ───│◄── {response, message_ids} ─────┤ status: working
      │                        │── POST …/hook/ack ─────────────►│
      │ uses a tool            │                                 │
      ├─ PostToolUse (stdin) ─►│── POST …/hook ─────────────────►│ urgent mail?
      │                        │◄── {response, message_ids} ─────┤
      │◄─ additionalContext ───│                                 │
      │                        │── POST …/hook/ack ─────────────►│ mark delivered
      │                        │                                 │
      │ wants to end its turn  │                                 │
      ├─ Stop (stdin) ────────►│── POST …/hook ─────────────────►│ urgent + normal?
      │◄─ decision: block ─────│◄── {response, message_ids} ─────┤ status: working
      │   reason: the mail     │── POST …/hook/ack ─────────────►│
      │ deals with the mail,   │                                 │
      │ wants to end again     │                                 │
      ├─ Stop ────────────────►│── POST …/hook ─────────────────►│ nothing → idle
      │◄─ (no output) ─────────│◄── {message_ids: []} ───────────┤
      │ stops                  │                                 │
```

The endpoints are described in `api/openapi.yaml` (`handleHookEvent`, `acknowledgeHookDelivery`).

## Formats exchanged with Claude Code

Event received (only the fields we use are listed, see `hook.Event`):

```json
{ "hook_event_name": "Stop", "session_id": "…", "cwd": "…", "stop_hook_active": false }
```

Answer on `Stop` when mail is waiting:

```json
{ "decision": "block", "reason": "Before ending your turn, read the mail below…" }
```

Answer on `PostToolUse` when urgent mail is waiting:

```json
{ "hookSpecificOutput": { "hookEventName": "PostToolUse", "additionalContext": "URGENT mail arrived…" } }
```

Answer on `SessionStart` for an agent that preloads skills:

```json
{ "hookSpecificOutput": { "hookEventName": "SessionStart", "additionalContext": "Skills preloaded for you…\n<preloaded_skill name=\"review\" dir=\"…\">…</preloaded_skill>" } }
```

No mail: the hook prints nothing, and the agent carries on.

Each delivered message shows its `ID`, its sender, its act ("an answer is expected" for `request`, `query` and `propose`, "no answer needed: do not reply" for the others) and reminds how to answer: the `send_message` MCP tool with `in_reply_to`.

## Guarantees and choices

- **At least once**: the daemon marks messages delivered only when `nobody-hook` confirms it printed the answer. A failure in between re-delivers a message rather than losing it.
- **No infinite loop**: a message leaves the tray as soon as it is delivered. When the agent tries to stop again, the tray is empty and the hook lets it go.
- **Isolation**: an agent can only acknowledge its own messages (the recipient is part of the update query).
- **Non-blocking errors**: if the daemon is unreachable, `nobody-hook` exits with code 1. Claude Code shows the error but the agent keeps working.
- **Configuration through the environment**: the daemon starts each agent with `NOBODY_AGENT` (its address) and `NOBODY_DAEMON_URL` (its project daemon's own address, a port the launcher chose: `projects.md`).
- **Never start an agent with `--bare`**: that mode disables hooks.

## Lesson: tell the agent that mail may arrive

A model handed mail without being told about it may take it for a **prompt injection attempt**, especially if the message mentions a tool it does not have. Observed with Haiku during the contract tests. Each agent's system prompt therefore explains that it belongs to an organisation, that its mail reaches it through these hooks, and that it has `send_message` to answer.

## Verification

These Claude Code behaviours are checked against the real CLI by `task contract` (`test/contract`), with a real daemon started in memory. Deterministic checks (was the mail delivered?) are strict; the model's reaction gets a second attempt, because a small model occasionally ignores an instruction. Rerun after every Claude Code upgrade.
