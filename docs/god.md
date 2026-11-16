# The director ("god")

The director talks with you, turns your requests into features and tickets, and leads the teams. It is an agent like the others, with no team and more rights. Decisions and rationale: ADR-0005.

## Setting it up

- A new project gets one from `nobodyd init` (recruited from the built-in `god` template).
- An existing project: Recruitment tab, template `god`, team "the director (no team)" (or `POST /api/director` with `{"name": "god", "template": "god"}`).

Its file is `.nobody/director/<name>.md`, and its address is that name: name the file `boss.md` and everyone writes to `boss`. There is at most one. Edit it like any agent (Recruitment tab, select it in the team list); its template uses Sonnet and the `default` permission profile.

## Working with it

The control room has a tab named after the director: your conversation with it, and a box to write to it. While its latest question waits for an answer, what you write answers it.

A typical request goes like this:

```
you ──── request ────────────────────────► god
    ◄─── query (what about…?) ─────────────
    ──── answer ───────────────────────────►
    ◄─── propose: feature F-3 ─────────────     (need + acceptance criteria)
    Start (or Refuse with a reason)
    ──── agree ────────────────────────────►    the teams restart in the feature's worktrees
                                                god splits the work into tickets for the teams,
                                                assigns them, follows the progress
```

1. **The need.** It clarifies your request until the need and its acceptance criteria are clear and testable, reading the project and the documentation MCP servers it was given (add them to its definition, e.g. `mcp: [ragflow, gitlab]`, once declared in `.nobody/mcp.yaml`).
2. **The proposal.** It proposes a feature with `feature_propose`. Nothing happens yet: the proposal shows in its tab and in the Feature panel, with **Start** and **Refuse**.
   - **Start** creates the feature branch and the teams' worktrees, makes it the active feature, restarts the team agents there (not the director, which keeps the conversation), and answers `agree` to the director.
   - **Refuse** answers `refuse` with your reason; it can work on it with you and propose again.
3. **The work.** It splits the feature into tickets for the teams (dependencies, acceptance criteria, assignee when obvious), follows them with `org_overview`, unblocks and arbitrates, and brings you the decisions that are yours.

## What it may do

| | Team agent | Director |
|---|---|---|
| Write to | its team, teams it has a channel to, the human, the director; answer anyone | anyone |
| Create tickets for | its team and teams it has a channel to | any team, with an assignee |
| Change a ticket's status | its own tickets | any ticket |
| Propose a feature | no | yes (`feature_propose`); you start or refuse it |
| Recruit, create, edit, put on leave, dismiss agents | no | yes, freely; never itself |

Its own tools, on top of the shared ones (`send_message`, `list_contacts`, `ticket_*`):

| Tool | Does |
|---|---|
| `feature_propose` | Proposes a feature (title, need, acceptance criteria, base branch). |
| `org_overview` | The active feature, and for each team its agents (status, on leave) and tickets (open, in progress, done and canceled counts). |
| `template_list`, `agent_get` | The catalog; an agent's definition. |
| `agent_recruit`, `agent_create` | Recruit from a template, or create from a definition written from scratch, into a team. |
| `agent_edit` | Replace an agent's definition (a new role or new instructions restart it with a fresh session). |
| `agent_leave`, `agent_return`, `agent_dismiss` | As in the Recruitment tab (`docs/agents.md`, "Lifecycle"). |

Changes to agents are written by the daemon (`internal/staffing`), as when you make them; the director cannot change its own definition, leave or dismiss itself. When a change is its own idea, its instructions ask it to say so and, before creating an agent from scratch, to show you the whole definition.

## Code

| Piece | Role |
|---|---|
| `internal/org/director.go` | The director in the organization: its file, its address, at most one. |
| `internal/messaging/policy.go` | It writes to anyone, anyone writes to it. |
| `internal/work/service.go`, `feature.go` | Its ticket privileges; feature proposals (`ProposeFeature`, `StartProposedFeature`, `RefuseFeature`). |
| `internal/officemcp/director.go` | Its office tools, exposed at its address only. |
| `internal/daemon/director.go`, `proposals.go` | The tools' backing (staffing, overview); proposals and decisions as messages. |
| `internal/catalog/builtin/god.md` | Its template. |
| `web/src/components/GodView.tsx`, `ProposalCard.tsx` | Its tab; proposals with Start / Refuse. |
