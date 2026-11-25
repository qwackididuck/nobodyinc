# Nobody Inc

Run teams of Claude Code agents that talk to each other (normal and urgent mailboxes, open flows between teams that each team can restrict), supervised from a web control room.

> Early stage project. Architecture: [`docs/architecture.md`](docs/architecture.md) · Projects: [`docs/projects.md`](docs/projects.md) · Desktop app: [`docs/desktop.md`](docs/desktop.md) · Hooks: [`docs/hooks.md`](docs/hooks.md) · Approvals: [`docs/approvals.md`](docs/approvals.md) · Tickets: [`docs/tickets.md`](docs/tickets.md) · Agents: [`docs/agents.md`](docs/agents.md) · Director: [`docs/god.md`](docs/god.md) · Workspaces: [`docs/workspaces.md`](docs/workspaces.md) · Scheduled tasks: [`docs/schedules.md`](docs/schedules.md) · Decisions: [`docs/adr/`](docs/adr/).

## Install

Download the latest build from the [releases](https://github.com/qwackididuck/nobodyinc/releases). Nobody Inc starts Claude Code agents, so [Claude Code](https://claude.com/claude-code) must be installed and authenticated (`claude` on your `PATH`).

### Desktop app

| Platform | File | Notes |
|---|---|---|
| macOS 13+ (Apple silicon, Intel) | `NobodyInc_<version>_macos_universal.dmg` | Open it, drag Nobody Inc to Applications. The app is not signed by Apple: the first time, allow it in System Settings > Privacy & Security > **Open Anyway** (or `xattr -dr com.apple.quarantine "/Applications/Nobody Inc.app"`). |
| Linux (amd64, arm64) | `NobodyInc_<version>_linux_<arch>.tar.gz` | Needs GTK 3 and WebKitGTK 4.1 (Debian/Ubuntu: `sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0`). |

On Linux:

```bash
mkdir -p ~/.local/opt && tar -C ~/.local/opt -xzf NobodyInc_<version>_linux_amd64.tar.gz
mkdir -p ~/.local/bin && ln -sf ~/.local/opt/NobodyInc/nobody-desktop ~/.local/bin/nobody-desktop
nobody-desktop
```

The three binaries in the archive (`nobody-desktop`, `nobodyd`, `nobody-hook`) must stay side by side. For an entry in your applications menu, copy `nobody-inc.desktop` to `~/.local/share/applications/` and `nobody-inc.png` to `~/.local/share/icons/`.

### Launcher alone (macOS, Linux)

`nobodyd_<version>_<os>_<arch>.tar.gz` holds `nobodyd` and `nobody-hook`, with the control room embedded. Extract it, keep both side by side, and run `nobodyd`: the control room is at http://127.0.0.1:7420.

### Verify a download

Each release has a `checksums.txt`: `sha256sum -c --ignore-missing checksums.txt` (`shasum -a 256` on macOS).

## Build from source

Requirements: Go 1.27+, Node 22+, pnpm, [Task](https://taskfile.dev), golangci-lint v2. The Linux desktop app also needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

```bash
task web:install
task desktop:build        # macOS: desktop/build/bin/Nobody Inc.app
task desktop:build:linux  # Linux: desktop/build/bin/NobodyInc_linux_<arch>.tar.gz
```

The launcher alone, in a browser:

```bash
task web:install
task web:export     # embeds the control room in the binaries
task build          # bin/nobodyd and bin/nobody-hook
bin/nobodyd         # then open http://127.0.0.1:7420
```

For development, `task dev:daemon` (the launcher, on 127.0.0.1:7420) and `task dev:web` (the control room on http://localhost:3000, reloaded as you edit).

## Getting started

From the control room, open a folder (Nobody Inc sets it up: `.nobody/` with the director, and Claude Code's trust) or create a new project, then build the teams. Each window shows one project, which runs while the window is open: closing it stops the project's agents (see [`docs/projects.md`](docs/projects.md)).

The organization lives in the project's `.nobody/` directory, committed with it: teams (with the teams each one blocks) and one markdown file per agent (see [`docs/agents.md`](docs/agents.md)). Your own agent templates go in `~/.nobody/catalog/`. Agents get the project's Claude Code configuration (`CLAUDE.md`, skills, commands, subagents), every MCP server of the project's `.mcp.json` and of your own Claude Code configuration, and your personal skills; each agent can exclude some, and preload skills it always needs. Recurring work (watch a dashboard every 10 minutes, a daily report) is a scheduled task in `.nobody/schedules.yaml`, handed to an agent while the project is open (see [`docs/schedules.md`](docs/schedules.md)).

Features (branches and worktrees) need the project to be a git repository. Agents work there with the default permission mode: their permission requests show up in the control room, where you allow or deny them.

## Development

```bash
task check      # lint + tests, before every commit
task contract   # Claude Code behaviours we rely on, against the real CLI (a few cents)
```

## License

[MIT](LICENSE).
