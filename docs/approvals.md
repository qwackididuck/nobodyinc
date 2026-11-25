# Approvals: agents' permission requests in the UI

Agents run with Claude Code's default permission mode: running a command, editing a file or calling an unlisted tool needs a permission. Instead of leaving that prompt hidden in the agent's terminal, Nobody Inc brings it to the control room, where the human allows or denies it: it shows as a card in the agent's chat, with a badge on its avatar and a "!" above its desk in the office.

## Flow

```
Agent (claude)                nobody-hook              nobodyd daemon                 UI
      │ needs a permission         │                          │                         │
      ├─ PermissionRequest ───────►│── POST …/hook ──────────►│ files the request       │
      │  (tool_name, tool_input)   │   (blocks, ≤ 9 min)      │ status: awaiting_approval
      │                            │                          │◄── GET /api/approvals ──┤
      │                            │                          │◄── POST /api/approvals/{id}
      │                            │◄── allow / deny ─────────┤     {"decision":"allow"}
      │◄─ decision.behavior ───────│                          │ status: working         │
      │ runs the tool, or reads    │                          │                         │
      │ the refusal message        │                          │                         │
```

- The hook answers Claude Code with `{"hookSpecificOutput": {"hookEventName": "PermissionRequest", "decision": {"behavior": "allow" | "deny", "message": "…"}}}`. A refusal's message is shown to the agent, so a short reason helps it change course.
- **No decision in time**: the daemon gives up after 9 minutes (Claude Code's hook timeout is set to 10) and answers nothing. Claude Code then shows its usual prompt in the agent's terminal, which the human can still answer from the UI.
- **Daemon restart**: the queue lives in memory (`internal/approval`). A pending request then fails over to the terminal prompt in the same way.
- **Daemon stopping** (closing the project's window, quitting the app): the daemon abandons every pending request the moment it is told to stop (`Queue.Close`), so that the agents fall back to their terminal at once. Their open hook requests would otherwise keep the daemon's graceful shutdown waiting, up to 10 seconds, with the control room frozen.

## Suggestions: "allow and…"

Claude Code's own prompt often offers more than yes/no: always allow this command, switch to auto mode, add a directory. The PermissionRequest event carries these as `permission_suggestions` (permission updates: `addRules`, `setMode`, `addDirectories`…, each with a `destination`: this session, the project's local or shared settings, or the user's). The queue keeps them on the request (`suggestions`) and the card shows one extra button per suggestion ("Allow and switch to auto mode (this session)"), saying where the change would be kept.

Picking one sends `POST /api/approvals/{id}` with `{"decision":"allow","suggestion":<index>}`; the hook then answers `allow` with that same update as `updatedPermissions`, echoed as received. A suggestion with a deny, or an index out of range, is a 400.

## Answered in the terminal

The human can also answer Claude Code's prompt in the agent's own terminal. The daemon learns it from the agent's next events (`Queue.Withdraw`, `withdrawAnswered` in `internal/server/hooks.go`): a `PostToolUse` of the same tool with the same input withdraws that request; a `Stop` or a `UserPromptSubmit` withdraws all the agent's requests (a refusal in the terminal leaves no other trace, so its card leaves when the turn ends). The card then goes from the chat.

## Pieces

| Piece | Role |
|---|---|
| `internal/approval` | The queue: `Ask` blocks until a decision or a timeout, `Decide` answers, `Pending` lists. |
| `internal/server/approvals.go` | Turns a `PermissionRequest` hook event into a queued request; serves `GET /api/approvals` and `POST /api/approvals/{id}`. |
| `internal/runtime/command.go` | Wires the agent's `PermissionRequest` hook to `nobody-hook` with a 10-minute timeout. |
| `web/src/components/ApprovalQueue.tsx` | Shows pending requests (the command for Bash, the file for edits) with Allow / Deny and an optional reason. |

## Notes

- Read-only commands that Claude Code allows on its own (e.g. `echo`, `ls`) never reach the queue: only real permission prompts do.
- The approval endpoints are on the daemon's loopback address, like the rest of the API. Anyone who can reach the daemon can decide; authentication comes with the VPS mode.
- Checked against the real CLI by `task contract` (`TestPermissionRequestUsesTheApprovalQueue`), which also shows the hook fires in non-interactive `-p` mode.
