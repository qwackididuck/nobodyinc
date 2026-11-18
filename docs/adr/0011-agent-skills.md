# ADR-0011 — Agent skills: every skill to every agent, with exclusions and preloading

- Status: accepted
- Date: 2026-11-18
- Gives a meaning to: the `skills:` field of the agent file (ADR-0004), "not applied yet" until now
- Revisits, in part: ADR-0003 (the user's personal skills reach the agents again; their settings, hooks and plugins still do not)

## Context

An agent is started with `--setting-sources project,local` (ADR-0003). With these flags, the project's skills (`.claude/skills/`) and Claude Code's built-in skills (`loop`, `simplify`, `code-review`…) reach it; the user's personal skills (`~/.claude/skills/`) do not, as they belong to the `user` source, which would also bring back the user's settings, hooks and plugins. The `skills:` field of an agent file is parsed but does nothing.

The human wants to see the skills in the control room, create one from it, and, per agent, exclude a skill or preload it.

Checked against the real CLI by `test/contract/skills_test.go` (Claude Code 2.1.327):

- a directory given with `--add-dir` brings its own `.claude/skills/`, symbolic links to skill directories included. `--add-dir` also gives the agent access to that directory's files;
- `skillOverrides: {"<name>": "off"}` in `--settings` takes a skill away from the model, built-in or not. A deny rule `Skill(<name>)` (or `--disallowedTools`) blocks the invocation but leaves the skill listed, and the model keeps asking for it;
- in a subagent definition, `skills:` means *preload*: the skill's content is loaded when the subagent starts; it does not restrict the other skills. It preloads nothing when that agent runs the main thread (`--agents` and `--agent`), which is how our agents run;
- a `SessionStart` hook can hand content to the session (`additionalContext`), also when a session is resumed, and the session keeps its memory. The system prompt, on the contrary, is ignored on resume (`TestResumeKeepsMemoryAndFollowsNewFlags`);
- a session's `init` event lists every skill even when the API cannot be reached (`ANTHROPIC_BASE_URL` on a closed port): the list of built-in skills can be read without calling the model. Killing a normal session right after `init` would not do, as its first API request leaves slightly earlier;
- invoking a skill whose frontmatter declares `hooks` asks for permission (a skill without hooks does not); once allowed, its hooks run for the session.

## Decision

1. **Three sources.** An agent gets:
   - the project's skills, `.claude/skills/` of the directory it works in (native);
   - Claude Code's built-in skills (native);
   - the user's personal skills, `skills/` of Claude Code's configuration directory (`$CLAUDE_CONFIG_DIR`, else `~/.claude`), through a staging directory: the daemon keeps, in the project's data directory, `~/.nobody/projects/<id>/skills/.claude/skills/`, one symbolic link per personal skill, and starts every agent with `--add-dir` on it. A personal skill named like a project skill is left out: the project wins, as for MCP servers (ADR-0010).
   Plugins' skills stay out, with plugins (ADR-0003).
2. **Every agent gets every skill.** The agent file may exclude some (`skills_exclude: [name, …]`, built-in skills included), applied with `skillOverrides: {"<name>": "off"}` in the agent's generated `--settings`. An exclusion naming a skill absent from this machine is ignored.
3. **`skills:` keeps Claude Code's meaning: preload.** The content of each listed skill (its `SKILL.md`, frontmatter left out, with the path of its directory for the files it refers to) is handed to the agent by its `SessionStart` hook (`nobody-hook`, asking the daemon), at every session start: new, resumed, compacted or cleared. So a change of an agent's preloaded skills restarts it once idle **resuming its session**: it keeps its memory. A skill both preloaded and excluded is refused when the file is validated; a preloaded skill absent from this machine is skipped with a warning. A skill removed from `skills:` stays in the history of a resumed session.
4. **The daemon discovers the built-in skills** by starting `claude -p --output-format stream-json` with the agents' flags and the API out of reach, reading the `init` event, and killing the process. Nothing leaves the machine. Built-in skills are those of that list that are neither project nor personal skills. The list is read at start-up and when the human asks for a refresh; it follows Claude Code's version and account.
5. **The control room lists, creates, edits and deletes skills.** A new skill goes, after an explicit answer, either *to the whole project* (`.claude/skills/<name>/SKILL.md`, to be committed by the human) or *to me, on all my projects* (personal skills). The daemon writes the file itself: Claude Code has no command for skills, and the format is a plain markdown file with a YAML header (`name`, `description`, then free keys kept as they are). Built-in skills are read-only.
   - A personal skill is clearly marked as such ("personal, all your projects"), and editing it warns that the change applies to every project and to the user's own Claude Code.
   - A personal skill can be **copied to the project** (`.claude/skills/<name>/`, to be committed). The copy then takes precedence over the personal skill for this project's agents (point 1), and each version is edited on its own: the project one, or the personal one.
   - Only the human changes skills, through the control room. Agents keep their deny rules on `.claude/`, and get the same (writes only; reading stays allowed) on the staging directory and on the personal skills directory: otherwise an agent could edit the user's personal skills through the symbolic links.
6. **Skills that declare hooks keep Claude Code's behaviour.** Their invocation asks for permission, so it reaches the human's approval queue (`docs/approvals.md`), and the hooks run only once approved. The control room shows these skills with a "declares hooks" badge.
7. **Worktrees: the human commits.** During a feature, an agent sees only the project skills committed on the feature's branch (`docs/workspaces.md`). The control room warns, for a project skill not committed, that agents working on a feature will not see it.
8. **A change reaches running agents once idle.** A skill added, removed or changed through Nobody Inc, or a change of an agent's `skills` or `skills_exclude`, restarts the agents concerned once idle, resuming their session. Files edited outside Nobody Inc are picked up at the next change made through it, or when the daemon restarts, as for agent files.

## Not decided here

- Importing a skill from elsewhere (a URL, a marketplace, `~/.claude/plugins`), and the security review it would need.
- The director creating or assigning skills.
- Per-team skills.
- `tools:` of the agent file: here it means "allowed without asking" (`--allowedTools`), not the restrictive list it is in a Claude Code subagent definition. To settle separately.

## Consequences

- The skills the human already wrote for themselves serve their agents too, in every project; the project's committed skills stay the shared part.
- Two developers on the same project may give their agents different skills (their personal ones). Agent files, with their `skills` and `skills_exclude`, stay the same for both.
- ADR-0003's isolation narrows a little further: the user's personal skills reach the agents. A skill is instructions the model reads; the one way it runs code by itself, its `hooks`, goes through the human's approval at each invocation.
- Agents get `--add-dir` on the staging directory, a `SessionStart` hook, and `skillOverrides` in their settings. Nobody Inc reads `SKILL.md` files it does not own, and must follow their format; the contract tests guard how Claude Code loads skills.
- Every agent start gains a call from the `SessionStart` hook to the daemon; the built-in skills cost a short `claude` start, without the model, at daemon start-up.
