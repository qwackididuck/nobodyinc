# Nobody Inc

Orchestration of teams of Claude Code agents: teams, inter-agent messaging (normal/urgent mailboxes), tickets with deliverables, no-code blueprint, web supervision. Decisions: `docs/adr/`. The roadmap (`docs/roadmap.md`) is a local, unversioned file.

## Stack
Overview and the journey of a message: `docs/architecture.md`.
- `cmd/nobodyd`: Go daemon (Go 1.27). Starts the agents, owns the mailboxes, serves the API described in `api/openapi.yaml` (update it before changing any endpoint).
- `cmd/nobody-hook`: relay run by the agents' Claude Code hooks (`docs/hooks.md`).
- `internal/`: `org` (the project's `.nobody/`: teams and their block lists, agent files, project and team prompts; the MCP servers every agent gets), `mcpservers` (the MCP servers of Claude Code's three scopes, ADR-0010, `docs/agents.md`), `catalog` (agent templates and the agent file format, `docs/agents.md`), `atomicfile` (crash-safe file writes), `settings` (the user's `~/.nobody/settings.yaml`), `staffing` (recruit, edit, leave, dismiss agents), `messaging` (sending rules: open flows, a team's block list, ADR-0009), `store` (SQLite, versioned migrations in `store/migrations/`: never edit a shipped one), `work` (tickets and features, `docs/tickets.md`), `workspace` (git branches and team worktrees in the target project, finishing a feature, `docs/workspaces.md`), `hook` (what to hand the agent), `approval` (permission requests waiting for the human, `docs/approvals.md`), `events` (changes pushed to the control room over SSE, `docs/events.md`), `officemcp` (the agents' MCP; the director's tools, `docs/god.md`), `runtime` (PTY, statuses, wake-up, terminal), `server` (HTTP).
- `internal/daemon`: wires everything together; used by `cmd/nobodyd` and by the end-to-end tests.
- `test/contract`: checks against the real Claude Code CLI the behaviours we depend on (hooks, `--fork-session`, `--strict-mcp-config`, PTY, permission requests, what agents get from the project, MCP scopes).
- `test/e2e` + `test/fakeclaude`: whole multi-agent scenarios through the real daemon, hooks and PTYs, with a deterministic fake `claude` (free, part of `task check`). Add a scenario here for every new cross-cutting behaviour.
- `web/`: Next.js 16 control room (App Router, strict TypeScript, pnpm). Read `web/AGENTS.md`: this Next.js version has breaking changes, its docs are in `web/node_modules/next/dist/docs/`.

## Commands
- `task check`: lint + tests, must pass before every commit. It needs `task web:install` first (pnpm: the lint runs `next typegen`, `tsc` and eslint) and golangci-lint v2.15.0.
- `task test` / `task lint` / `task build`
- `task contract`: contract tests against the real CLI (costs a few cents); rerun after every Claude Code upgrade.
- `task dev:daemon PROJECT=<dir>`: builds and runs the daemon on a project holding `.nobody/` (default `../nobody-sandbox`; create one with `./bin/nobodyd init <dir>`); listens on 127.0.0.1:7420, override with `NOBODY_ADDR`. The database goes to `~/.nobody/projects/<id>/` (`NOBODY_HOME` replaces `~/.nobody`).
- `task dev:web`: control room on http://localhost:3000.
- `task web:install`
- CI (`.github/workflows/ci.yml`): every branch runs golangci-lint (v2.15.0, `.golangci.yml`, adapted from ring-go-dev), `go vet` and `go test -race`; main also builds the binaries and publishes a GitHub release tagged from the conventional commits. Contract tests are not run in CI.

## Conventions
- **Readability first**: package docs that explain how things work, small well-named functions, doc comments on exported identifiers, and a `docs/` page for every cross-cutting mechanism.
- Hooks, integrations and glue: documented, tested Go code, never bash scripts or one-liners.
- Table-driven tests (`t.Run`) for non-trivial logic.
- Never start an agent with `--bare` (it disables hooks).
- Branches: `<iid>-<description>` when an issue exists, otherwise a short description (e.g. `ticket-worktrees`; no tracker yet), for features → `develop` (collects features) → `main` (merging is the decision to release). Use conventional commits: they decide the next version.
- Logging through `log/slog`, never a permanent `fmt.Println`.
- The daemon listens on loopback only by default.
- Structured data about agents comes from hooks and OTel only, never from reading the PTY output. The only thing written to the PTY is keystrokes (wake-up, human input).
- Agents are isolated from the user's global settings, hooks and plugins, and from a parent session's environment (ADR-0003). From the user, only the text of their CLAUDE.md (ADR-0008) and their MCP servers (ADR-0010) reach them; from the project, its Claude Code configuration (`CLAUDE.md`, skills, commands, subagents, `.mcp.json`; `docs/agents.md`). Do not change this without a new ADR.
- An agent's system prompt is a cascade (`docs/agents.md`): user's CLAUDE.md → `.nobody/project.md` → team goal and `TEAM.md` → the agent's own file. Built-in templates are about a trade, never about a project.
- To test with real agents, use a throwaway git repository as the project, never a real one. Ready-made: `~/Documents/workspace/nobody-sandbox`, trusted in Claude Code; its `.nobody/` is not versioned and may hold only the director (Opus): create a team and recruit Haiku agents from the control room before testing. `task dev:daemon`, then `task dev:web`; `./bin/nobodyd -project ../nobody-sandbox -start=false` tests the control room without starting (or paying for) agents. A new project must be trusted once in Claude Code (`docs/workspaces.md`).
- The daemon reads and, through the MCP endpoints, writes the user's real `~/.claude.json` (user and local MCP scopes): test MCP changes with the project scope, or with a throwaway `CLAUDE_CONFIG_DIR` when agents need not start (the project's trust is stored there too). Tests never read the real file (`TestMain` points `CLAUDE_CONFIG_DIR` to an empty directory).
- Commits, pushes and pull requests are done by the maintainer: modify files, do not commit. At each stopping point, give a one-line conventional commit message (no body). Pull requests target `develop`.
- `docs/roadmap.md` (local, unversioned, in French) is the source of truth for the work in progress, the decisions taken with the maintainer and what comes next: read it at the start of a session, and update it when a step ends.
- Documentation is written in English.
