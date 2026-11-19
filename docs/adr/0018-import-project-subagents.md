# ADR-0018 — Importing the project's subagents as agents

- Status: accepted
- Date: 2026-11-19
- Builds on: ADR-0004 (agent files), ADR-0011 (`skills:` keeps Claude Code's meaning), ADR-0014 (setting a folder up)

## Context

A project onboarded into Nobody Inc may already have Claude Code subagents, `.claude/agents/*.md`: a YAML header (`name`, `description`, `tools`, `disallowedTools`, `model`, `permissionMode`, `skills`, `mcpServers`, `hooks`, `maxTurns`, `memory`, `color`…), then the subagent's system prompt. Agents already get them as subagents (the `project` settings source, `test/contract/project_test.go`), but Nobody Inc neither shows them nor offers to make them members of a team.

A subagent's identity is its `name`, not its file name ("The filename doesn't have to match", Claude Code's documentation). Its fields are close to an agent file's (ADR-0004), with one trap: its `tools` is a **restriction** (the subagent only gets those tools), while an agent file's `tools` are tools allowed **without asking**, on top of the profile.

The human wants, when such a project is onboarded, a window listing these subagents, to create teams and put the subagents in them, as agents in their own right.

## Decision

1. **Copy, keep the original.** Importing a subagent writes `.nobody/teams/<team>/<agent>.md` from it, with `from: subagent:<name>`. `.claude/agents/` is never touched: the subagent stays usable as a subagent, by every agent and by the human's own Claude Code. The two then evolve apart.
2. **Conversion** (`internal/subagents`):

   | Subagent | Agent file | |
   |---|---|---|
   | `name` | the agent's name, suggested: lower case, `_`, spaces and dots as `-`, other characters dropped | editable in the window; it must be a valid agent name, free in its team |
   | `description` | `role`: its first sentence, on one line; `expertise`: the description, shortened if long | fixed afterwards in Staff |
   | body | instructions | as is |
   | `model` | `model` | `inherit` or none: Claude Code's default |
   | `permissionMode` | `permissions.profile` | `acceptEdits` → `allowlist`; `auto` → `auto`, except with a Haiku model → `default`; anything else or none → `default`. Editable in the window |
   | `disallowedTools` | `permissions.deny` | the same meaning: forbidden |
   | `skills` | `skills` (preloaded) | the same meaning (ADR-0011); one absent from the machine is skipped at start-up with a warning, as for any agent |
   | `tools` | — | **not carried over**: its meaning is the opposite. The window shows the list, to set by hand in Staff |
   | every other key | — | not carried over, listed in the window: the agents already get every MCP server (ADR-0010), and their hooks are Nobody Inc's |
3. **Status of a subagent.** A subagent of the project's main checkout is *imported* when an agent has `from: subagent:<its name>`, *dismissed* when its name is in `ignored_subagents:` of `org.yaml` (committed: the whole team shares the decision), *pending* otherwise. A file that cannot be read is shown with its error and cannot be imported. Subagents of the user's own configuration (`~/.claude/agents`) are out of scope: onboarding is about the project.
4. **One import, all or nothing.** `POST /api/subagents/import` takes the new teams (id and goal), the agents (subagent, team, name, profile) and the subagents to dismiss. Everything is checked first, against the organization with all of it (names, teams, conflicts with existing agents or with each other, profiles); then `org.yaml` is written once (new teams, dismissed subagents), then the agent files, then the organization is reloaded once. Each new agent starts at once, with a fresh session, as a recruit does: each costs its onboarding turn, which the window's button says ("Create 2 teams, 3 agents").
5. **When the window opens.** It opens by itself the first time a project is opened right after the launcher set it up (`POST /api/projects` answers `created: true`; the control room opens the project with `setup=1`), if subagents are pending. Afterwards, pending subagents show as a count on the STAFF tab and a block at the top of the Staff screen, which opens the window. "Later" closes it without writing anything.

## Not decided here

- Keeping an imported agent in step with its subagent (a "linked" mode reading `.claude/agents/` at each start).
- Importing the user's personal subagents.
- Translating `tools` into deny rules to keep the restriction.

## Consequences

- A project's existing subagents become a starting organization in a few clicks, without retyping their prompts.
- An imported agent may get more tools than its subagent had (its `tools` restriction is dropped): the window says so, and the profile stays the safe default unless the subagent asked for `acceptEdits` or `auto`.
- `org.yaml` gains an optional `ignored_subagents:` list; a name in it whose subagent has gone stays until the human edits it (harmless).
- `POST /api/projects` of the launcher gains `created`; the control room reads `setup=1` once and drops it from the address.
