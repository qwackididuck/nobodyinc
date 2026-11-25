# ADR-0015 — Resume the agents' sessions when a project opens again

- Status: accepted
- Date: 2026-11-18

## Context

Since ADR-0014, a project runs only while a window holds it: closing the window stops its daemon and its agents. Every reopening used to start every agent with a fresh session. That was rare when the daemon ran for days; now it happens every time the human closes a window, and each agent loses what it knew: the conversation with the human, the ticket it was on, what it learnt about the code.

Claude Code keeps every session on disk (`~/.claude/projects/<directory>/<session>.jsonl`; agents use the user's Claude Code directory, ADR-0003 only isolates settings), and `claude --resume <session>` continues one. The daemon already resumes sessions, within one run: changing an agent restarts it with `--resume` and a first prompt carrying the current organization (`runtime.resumeMessage`, ADR-0004). What it lacks is memory: an agent's session ID (`runtime.Statuses`) and the directory it started in (`Supervisor.sessionDirs`) live in memory only.

Reusing that first prompt at every reopening would be expensive: each resumed agent would run a turn ("Ready.") that reads its whole history, uncached, at every opening of the project, even when nothing waits for it. The prompt cache only lasts a few minutes, so the first turn after a reopening is never cached anyway; the waste is running that turn for agents that have nothing to do.

## Decision

### 1. The daemon remembers each agent's session

A table `agent_sessions` (migration `0011`) keeps, per agent address: the Claude Code session ID, the directory the session started in, and whether the agent is in the middle of a turn. The session is written when a hook reports a new session ID for the agent (`Statuses.OnSession`, the `SessionStart` hook included), with the directory the supervisor started it in. The mid-turn flag follows the agent's status at every change (`working` and `awaiting_approval` set it, `idle` and `budget_hold` clear it; starts and exits leave it alone), so that it is right even when the daemon is killed.

Every fresh start of an agent forgets its remembered session (`Supervisor.command`): hired (an address may have belonged to a dismissed agent), edited with a change that needs a fresh session (`staffing.needsFreshSession`), "new session", moved to another directory (a feature that starts or ends restarts the team agents afresh), or a lost session. The new session is recorded once its first hook reports it. A dismissed agent's row stays until its address is hired again; it is never resumed, since only members of the organization start.

### 2. A silent resume when the project opens

When the daemon starts its agents (`StartAll`), an agent with a recorded session started in the directory it works in now is started with `claude --resume <session>` **and no prompt**. Claude Code runs the `SessionStart` hook (source `resume`); the daemon answers it as it does today (preloaded skills, ADR-0011), adds the current organization to the same `additionalContext` (it may have changed while the project was closed), and marks the agent `idle`, or `budget_hold` when a budget holds it (ADR-0013). Idle, it is woken up by mail like any other agent (mail waiting since before the start included, through `OnIdle`). Only the first `SessionStart` of the process counts: a later one (`/clear`, compaction) is not a reopening.

The cost is therefore that of the next real turn, which would have been uncached anyway.

Exceptions:

- **An agent interrupted in the middle of a turn** resumes with a short prompt telling it its session was interrupted and to carry on with what it was doing (with its mail, if any): that turn is the work it had not finished. It gets the current organization from its `SessionStart` hook too.
- **An agent whose session cannot be resumed** (none recorded, another directory, e.g. a feature started or ended meanwhile) starts with a fresh session and the onboarding turn, as today.
- **A session Claude Code no longer has**: `claude` exits at once. A resumed process that ends before its first `SessionStart` without being stopped by the supervisor is taken as a lost session: its row is forgotten and the agent starts afresh (`Supervisor.watch`).
- Changes made while the agent runs keep their current behaviour (`resumeMessage`, applied at the end of a turn): the agent is in the middle of the conversation and the prompt is cheap and cached.

### 3. Verified by a contract test first

The silent resume depends on behaviours of the real CLI, checked by `test/contract/resume_test.go` (2026-11-18, Claude Code 2.1.327) before the daemon relied on them:

- an interactive `claude --resume <id>` without a prompt runs `SessionStart` with source `resume` and the same session ID, then waits at its prompt: no turn, no cost;
- the `additionalContext` it returns reaches the next prompt typed in the terminal, and the session keeps its memory;
- a prompt typed as soon as `SessionStart` has run, before the TUI draws its prompt, is taken (so mail can wake the agent at once);
- resuming an unknown session prints "No conversation found with session ID: …" and exits with status 1 at once, without running `SessionStart`.

## Not decided here

- Resuming after a crash of an agent alone (automatic restart).
- Compacting long sessions.

## Consequences

- Closing a window no longer costs the agents their memory; reopening a project costs nothing until an agent works.
- A resumed agent keeps the system prompt of its first start (Claude Code ignores a new `--append-system-prompt` on `--resume`): organization changes reach it as context, as for changes made while it runs.
- Sessions accumulate history across openings; compaction is left to Claude Code.
- `test/fakeclaude` keeps its sessions on disk, supports `--resume` without a prompt and fails on an unknown session like the real CLI; `test/e2e/resume_test.go` covers a silent resume, an interrupted turn and a lost session.
