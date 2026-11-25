# Automatic mode

By default every sensitive action of an agent waits for the human (`approvals.md`). The automatic mode lets the daemon answer these requests itself, so that the agents work unattended.

## What it does

- `PUT /api/automation {"level":"auto"}` switches it on, `{"level":"off"}` off; `GET /api/automation` reads it. Switching it on also allows the requests already waiting.
- With the mode on, `askHuman` (`internal/server/approvals.go`) answers `allow` to every PermissionRequest at once: no card, no waiting. Each decision is logged (`slog`, warning level, with the tool's input).
- **No agent restarts.** Claude Code cannot change the `--permission-mode` of a running session, so the daemon decides in the hook instead. The agents' profiles are untouched. This is not Claude Code's own `auto` mode (no classifier): everything that would have asked is allowed.

## What it does not change

Claude Code evaluates its **deny rules before it ever asks for a permission**, so they still win in automatic mode: `.nobody/` and `.claude/` stay unwritable, and so do the outside directories of ADR-0004, and `git push` under the allowlist profile. Nothing else protects the machine: shell commands, file edits and MCP tools are allowed without review.

## Lifetime

The level lives in the daemon's memory (`internal/automation`). Closing the project's window or restarting the daemon puts it back to `off`, so a dangerous mode is never left on by forgetting about it.
