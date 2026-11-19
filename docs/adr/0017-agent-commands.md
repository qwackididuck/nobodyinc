# ADR-0017 — Agent commands: the project's and the user's, shown and edited in the control room

- Status: accepted
- Date: 2026-11-19
- Revisits, in part: ADR-0003 (the user's personal commands reach the agents, as their personal skills do since ADR-0011; their settings, hooks and plugins still do not)
- Follows: ADR-0011 (same staging directory, same rules for the project and personal scopes)

## Context

A command is a markdown file in `.claude/commands/` (the project's) or in `commands/` of Claude Code's configuration directory (`$CLAUDE_CONFIG_DIR`, else `~/.claude`: the user's personal ones), with an optional YAML header (`description`, `argument-hint`, `allowed-tools`, `model`, `disable-model-invocation`…). It is invoked as `/<name>`; a command in a sub-directory, `front/test.md`, is named `front:test`.

Agents are started with `--setting-sources project,local` (ADR-0003): the project's commands reach them, the user's personal commands do not. The control room shows none of them. The human wants to see the commands in Settings and create, edit and delete them, as skills (ADR-0011).

Checked against the real CLI by `test/contract/commands_test.go` (Claude Code 2.1.328), without calling the model:

- the project's commands are listed in the session's `init` event (`slash_commands`), a command in a sub-directory as `<dir>:<name>`;
- the user's personal commands are not listed with the agents' flags alone;
- a directory given with `--add-dir` brings its own `.claude/commands/`, through symbolic links to the command files, sub-directories included.

## Decision

1. **Two sources, as for skills.** An agent gets:
   - the project's commands, `.claude/commands/` of the directory it works in (native);
   - the user's personal commands, through the staging directory of ADR-0011: the daemon keeps, next to `.claude/skills/`, `~/.nobody/projects/<id>/skills/.claude/commands/`, one symbolic link per personal command **file**, sub-directories recreated as real directories. Agents already get `--add-dir` on that directory: no new launch flag. A personal command named like a project command (same `<dir>:<name>`) is left out: the project wins, as for skills and MCP servers.
   Plugins' commands stay out, with plugins (ADR-0003). Claude Code's built-in commands (`/compact`, `/init`…) are not listed by the control room.
2. **Every agent gets every command.** No per-agent exclusion for now (see "Not decided here").
3. **The control room lists, creates, edits and deletes commands**, in a "Commands" section of Settings, with the same choices as for skills: a new command goes, after an explicit answer, *to the whole project* (`.claude/commands/<path>.md`, to be committed by the human) or *to me, on all my projects*. The daemon writes the file itself (atomically). Header keys it does not know are kept as they are; a header that is not valid YAML is read line by line, as for skills.
   - A personal command is marked as such, and editing it warns that the change applies to every project and to the user's own Claude Code.
   - A personal command can be **copied to the project**; the copy then takes precedence for this project's agents.
   - A command's name is its path under `commands/` without `.md`, `/` written `:`. Each segment follows the same rule as a skill's name (lower-case letters, digits, `-`); the human can create a namespaced command (`front:test`).
   - Only the human changes commands. Agents keep their deny rules on `.claude/`, and get deny rules on writes to the personal commands directory, as for the personal skills directory (ADR-0011, point 5): otherwise an agent could edit the user's commands through the symbolic links.
4. **Worktrees: the human commits.** During a feature, an agent sees only the project commands committed on the feature's branch. The control room warns, for a project command not committed, that agents working on a feature will not see it.
5. **A change reaches running agents once idle**, as for skills (ADR-0011, point 9): a command added, removed or changed through Nobody Inc restarts the agents once idle, resuming their session (they keep their memory).

## Not decided here

- Excluding a command per agent (`commands_exclude`).
- Listing Claude Code's built-in commands, read-only, as built-in skills are.
- The director creating commands.

## Consequences

- The commands the human wrote for themselves serve their agents too, in every project.
- ADR-0003's isolation narrows a little further. A command is instructions the model reads, but its `allowed-tools` header lets the tools it lists run **without asking** while the command runs, and the model can invoke a command itself unless it declares `disable-model-invocation: true`. A personal command with `allowed-tools: Bash(...)` therefore widens what an agent may do without the approval queue. The control room shows an "allows tools" badge on such commands, with the list. (The same holds for a skill's `allowed-tools`, already true since ADR-0011.)
- Nobody Inc reads and writes command files it does not own, and must follow their format; `test/contract/commands_test.go` guards how Claude Code loads them, and must be rerun after every Claude Code upgrade (`task contract`).
