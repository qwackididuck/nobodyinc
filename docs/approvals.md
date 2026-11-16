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
