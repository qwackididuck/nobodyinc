# Automatic mode

By default every sensitive action of an agent waits for the human (`approvals.md`). The automatic mode lets the daemon answer these requests itself, so that the agents work unattended.

## What it does

- `PUT /api/automation {"level":"auto"}` switches it on, `{"level":"off"}` off; `GET /api/automation` reads it. Switching it on also allows the requests already waiting.
- With the mode on, `askHuman` (`internal/server/approvals.go`) answers `allow` to every PermissionRequest at once: no card, no waiting. Each decision is logged (`slog`, warning level, with the tool's input).
- **No agent restarts.** Claude Code cannot change the `--permission-mode` of a running session, so the daemon decides in the hook instead. The agents' profiles are untouched. This is not Claude Code's own `auto` mode (no classifier): everything that would have asked is allowed.

## Nobody level

`{"level":"nobody"}` is `auto` plus a second delegation: **the director answers, in the human's place, the questions the agents ask them.**

```
dev1 ── query ──► human         (a question: text, not a permission request)
          │
          └─ daemon (nobodyOffice.route) ── notice ──► god
                                                         │ decides, then decision_log
dev1 ◄── answer (from god) ──────────────────────────────┤
human ◄─ "answered by the director" (closes the card) ───┘
          decision kept in the report (GET /api/decisions)
```

- Only a `query` to the human is taken over, whoever asks, the director included (its own question is decided by itself: nothing is sent to anyone). **Proposals** (features, schedules, budgets) are not: they commit money or change the project's configuration, and keep waiting for the human.
- The notice (`internal/daemon/nobody.go`) tells the director to decide, prefer the most reversible option the human would choose, and ask the agent whose expertise covers a question about the project first.
- `decision_log` (`internal/officemcp/decisions.go`, the director's only): `message_id`, `decision`, `rationale`, `impact` (`low`, `medium`, `high`: high is what is hard to undo, costly, widening the scope or touching security). The daemon records it (`decisions` table, one per question), sends the decision to the asker as an answer, and answers the question's card for the human, which would otherwise stay open.
- The report: `GET /api/decisions` lists them newest first; `POST /api/decisions/seen` acknowledges some or all (`decisions.changed` event).

## What it does not change

Claude Code evaluates its **deny rules before it ever asks for a permission**, so they still win in automatic mode: `.nobody/` and `.claude/` stay unwritable, and so do the outside directories of ADR-0004, and `git push` under the allowlist profile. Nothing else protects the machine: shell commands, file edits and MCP tools are allowed without review.

## Lifetime

The level lives in the daemon's memory (`internal/automation`). Closing the project's window or restarting the daemon puts it back to `off`, so a dangerous mode is never left on by forgetting about it.
