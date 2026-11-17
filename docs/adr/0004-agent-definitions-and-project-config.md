# ADR-0004 — Agent definitions, catalog and per-project configuration

- Status: accepted; its channels are superseded by ADR-0009 (open flows with a block list), its MCP registry (`.nobody/mcp.yaml`) and per-agent `mcp:` list by ADR-0010
- Date: 2026-11-12

## Context

Until now the whole organization lived in one `nobody.yaml` next to the daemon: teams, channels, and for each agent a name, a role, a prompt and a model. That file is local to one machine, holds nothing about permissions, tools or MCP servers, and is read once at startup.

We want to:

- give every agent its own permissions, tools, MCP servers and skills (ADR-0003 cut agents off from the user's global configuration, so what they need must now be assigned explicitly);
- offer a catalog of ready-made agent templates that the user can extend without touching the code;
- recruit, edit, put on leave and dismiss agents while the daemon runs;
- let several people run Nobody Inc on the same project: the team setup must travel with the repository.

## Decision

### 1. Templates and agents are markdown files

An agent (and a template) is one markdown file: a YAML front matter compatible with Claude Code subagents, followed by the prompt. The format is described in `docs/agents.md`. Files are the source of truth: they can be edited by hand, and the UI only writes them.

### 2. Two places for templates, one for agents

| Place | Content | Versioned with |
|---|---|---|
| Inside the binary (`internal/catalog/builtin/`, `go:embed`) | Templates shipped with Nobody Inc | Nobody Inc |
| `~/.nobody/catalog/<template>.md` | The user's own templates, and overrides | Nothing (user's machine) |
| `<project>/.nobody/` | Teams, channels, MCP registry, recruited agents | The project's repository |

- A user template with the same name as a built-in one **replaces it entirely** (no field-by-field merge: what you see in the file is what you get).
- `~/.nobody/` is the user's global Nobody Inc configuration, created at installation. `<project>/.nobody/` is created when Nobody Inc is set up for a project, and is committed so that other people can run the same organization.
- Recruiting copies a template into a team. The agent keeps `from: <template>@<version>` for information only: **template updates are never propagated** to agents already recruited. A recruit is independent from the day it is hired.

### 3. Runtime state is per machine

The SQLite database (mail, tickets, approvals history) and anything specific to one machine are neither versioned nor shared. They live in `~/.nobody/projects/<id>/`, where `<id>` is derived from the project's absolute path. The teams' worktrees live there too (`worktrees/`), outside the project, so that the project's `.nobody/` can be protected by absolute path (§4). Claude Code's trust of the project still covers them: git ties a worktree to its repository (checked by experiment, 2.1.322).

### 4. Agents cannot change their own configuration

`<project>/.nobody/` is in the repository, so it also appears in every team worktree. An agent able to edit it could widen its own permissions and deliver that change with its ticket. Therefore:

- the daemon reads the configuration **only from the project's main checkout**, never from a worktree;
- every agent is launched with `deny` rules on `Edit` and `Write` of `.nobody/**` and `.claude/**`, whatever its profile, both relative to its working directory (`./`) and by absolute path to the project's main checkout (`//<project>/`). `.claude/` is included because agents load project settings (`--setting-sources project,local`): writing `.claude/settings.local.json` would widen their permissions at their next restart. The absolute rules are what protects the main checkout from an agent working in a worktree (checked by `task contract`);
- the `allowlist` profile allows only the git commands needed to commit (`status`, `diff`, `log`, `show`, `add`, `commit`), not `git *`: `checkout`, `restore` or `apply` could write any path, the main checkout's `.nobody/` included;
- a delivery (ticket closure) whose commits touch `.nobody/` is caught. The user's setting `config_protection` in `~/.nobody/settings.yaml` decides how: `block` (default) refuses the delivery until the agent undoes the change; `warn` delivers and warns the human, who must review the change before merging the feature.

These rules make the change hard, not impossible: an agent allowed to run code it wrote (tests, scripts) can still do anything the user can. Only a sandbox closes that, which is why the `yolo` profile waits for one (§5). For the same reason, the daemon's API is only protected against requests a web page could send (another `Origin`, or a body that is not JSON); an authenticated API comes with the VPS mode.

Only Nobody Inc itself (daemon, UI) writes `.nobody/`. The god session, when it exists, will change the configuration through office MCP tools, so the daemon remains the only writer.

### 5. Permission profiles

Each agent has a permission profile that sets Claude Code's `--permission-mode` explicitly at launch (which also removes the "auto mode" question seen at startup):

| Profile | Launch | Meaning |
|---|---|---|
| `default` | `--permission-mode manual` | Every sensitive action goes to the approval queue. |
| `allowlist` | `--permission-mode acceptEdits` + preset allow rules (`git status/diff/log/show/add/commit`, usual test commands; `git push` denied) | File edits in the working directory, committing and tests run without asking; everything else goes to the approval queue. |
| `auto` | `--permission-mode auto` | Claude Code's classifier decides. Not available with Haiku (Claude Code silently falls back to `manual`), so this combination is refused. |

The agent's own `allow` / `deny` rules are added on top, and the `.nobody/` and `.claude/` deny rules always apply. A `yolo` profile (`--dangerously-skip-permissions`) is **postponed**: it will only exist together with Claude Code's sandbox, once a contract test proves that the sandbox keeps writes inside the worktree.

### 6. MCP servers per agent

The project's MCP servers are declared in `<project>/.nobody/mcp.yaml`. Since the file is committed, secrets are never written in it: they are referenced as environment variables. An agent lists the servers it may use; it is launched with `--mcp-config` holding only those servers plus the office server, and `--strict-mcp-config` so that no other MCP configuration (such as the project's `.mcp.json`) is loaded.

### 7. Changes apply between turns

Editing an agent restarts it with `claude --resume <session>` and its new configuration as soon as it is idle, never in the middle of a turn. The same applies to putting it on leave or dismissing it. An explicit "restart from scratch" option starts a fresh session instead, and a "now" option does not wait for the end of the turn (for a stuck agent).

Claude Code keeps the system prompt a session started with: on `--resume` it ignores a new `--append-system-prompt` (while the model, permission mode, tools and MCP servers do follow the new flags), and the model rightly refuses new instructions handed over in a message (checked by `task contract`). So a change of **role or instructions restarts the agent with a fresh session**; any other change (model, permissions, tools, MCP, leave and return) resumes it, with the current organisation handed over in its first message for information. A later option is to hand new instructions over through a `SessionStart` hook on resume, as system context, if losing the conversation turns out to matter.

## Consequences

- `nobody.yaml` is replaced by `<project>/.nobody/`. An existing `nobody.yaml` is migrated automatically on first start.
- The daemon is started with the project directory (`nobodyd -project <dir>`), not with an organization file.
- The organization can change while the daemon runs: every component reads it through a shared holder instead of keeping its own copy.
- Adding a template needs no code change: drop a markdown file into `~/.nobody/catalog/`. Adding a built-in template is a file in `internal/catalog/builtin/` and a rebuild.
- Behaviours checked against the real CLI (`task contract`, `test/contract/definition_test.go`): the three permission modes are accepted (auto only on non-Haiku models); deny rules on `.nobody/` win over `acceptEdits`; `${VARIABLE}` references in `--mcp-config` are expanded; a resumed session keeps its memory, follows a changed model, permission mode and MCP config, and obeys a definition handed over in its first message. Still to check: whether skills from `~/.claude/skills` stay visible with `--setting-sources project,local` (if not, skills are copied into the worktree's `.claude/skills/`). Until then, `skills` is not applied.
