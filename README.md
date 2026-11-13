# Nobody Inc

Run teams of Claude Code agents that talk to each other (normal and urgent mailboxes, one-way channels between teams), supervised from a web control room.

> Early stage project. Architecture: [`docs/architecture.md`](docs/architecture.md) · Hooks: [`docs/hooks.md`](docs/hooks.md) · Approvals: [`docs/approvals.md`](docs/approvals.md) · Tickets: [`docs/tickets.md`](docs/tickets.md) · Agents: [`docs/agents.md`](docs/agents.md) · Director: [`docs/god.md`](docs/god.md) · Workspaces: [`docs/workspaces.md`](docs/workspaces.md) · Decisions: [`docs/adr/`](docs/adr/).

## Requirements

Go 1.27+, Node 22+, pnpm, [Task](https://taskfile.dev), golangci-lint v2, Claude Code (authenticated).

## Getting started

```bash
task build                              # bin/nobodyd and bin/nobody-hook
./bin/nobodyd init /path/to/project     # creates .nobody/ with a first team
task web:install
task dev:daemon PROJECT=/path/to/project   # starts the agents
task dev:web                            # control room on http://localhost:3000
```

The organization lives in the project's `.nobody/` directory, committed with it: teams, channels and one markdown file per agent (see [`docs/agents.md`](docs/agents.md)). Your own agent templates go in `~/.nobody/catalog/`. A former `nobody.yaml` is migrated with `nobodyd -org nobody.yaml`.

The project must be a git repository already trusted in Claude Code (open it once with `claude` and accept the trust prompt). Agents work there with the default permission mode: their permission requests show up in the control room, where you allow or deny them.

## Development

```bash
task check      # lint + tests, before every commit
task contract   # Claude Code behaviours we rely on, against the real CLI (a few cents)
```
