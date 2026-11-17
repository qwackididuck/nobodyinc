# The director ("god")

The director talks with you, turns your requests into features and tickets, and leads the teams. It is an agent like the others, with no team and more rights. Decisions and rationale: ADR-0005.

## Setting it up

There is nothing to do: the director is always present. It is not in the catalog. `nobodyd init` writes it, alone (you then build the teams, or ask it to), and the daemon writes it when it starts on a project that has none (`org.EnsureDirector`, from `internal/catalog/director/god.md`).

Its file is `.nobody/director/<name>.md`, and its address is that name: rename the file `boss.md` and everyone writes to `boss`. There is exactly one. Edit it like any agent (Recruitment tab, Staff); it can be neither put on leave nor dismissed. Its built-in definition uses Opus and the `default` permission profile.

## Working with it

The director's chat comes first in the control room's avatar rail: your conversation with it, and a box to write to it. Its urgent questions show as cards you answer in place, and its proposals as cards with **Start** and **Refuse**.

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
2. **The proposal.** It proposes a feature with `feature_propose`. Nothing happens yet: the proposal shows in its chat, with **Start** and **Refuse**, and in the banner above the office.
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
| Create and delete teams | no | yes (`team_create`; `team_delete` only with your agreement) |
| Recruit, create, edit, put on leave, dismiss agents | no | yes, freely; never itself |

Its own tools, on top of the shared ones (`send_message`, `list_contacts`, `ticket_*`):

| Tool | Does |
|---|---|
| `feature_propose` | Proposes a feature (title, need, acceptance criteria, base branch). |
| `org_overview` | The active feature, and for each team its goal and prompt, its agents (role, expertise, status, on leave) and tickets (open, in progress, done and canceled counts). |
| `team_create` | Creates a team (id, goal, and the prompt its members share, which it writes from the goal). |
| `team_delete` | Deletes a team and dismisses its members; its instructions ask it to get your agreement first. |
| `template_list`, `agent_get` | The catalog; an agent's definition. |
| `agent_recruit`, `agent_create` | Recruit from a template, or create from a definition written from scratch, into a team. |
| `agent_edit` | Replace an agent's definition (a new role or new instructions restart it with a fresh session). |
| `agent_leave`, `agent_return`, `agent_dismiss` | As in the Recruitment tab (`docs/agents.md`, "Lifecycle"). |

It is not a source of knowledge about the project: it knows what you want, the teams know how the project works. The agents' routing rule treats a question to it like a question to you (`docs/agents.md`, "Whom to ask"), and its own prompt asks it to send questions about the project to the agent whose expertise covers them.

Changes to agents are written by the daemon (`internal/staffing`), as when you make them; the director cannot change its own definition, leave or dismiss itself. When a change is its own idea, its instructions ask it to say so and, before creating an agent from scratch, to show you the whole definition.

## Code

| Piece | Role |
|---|---|
| `internal/org/director.go` | The director in the organization: its file, its address, exactly one (`EnsureDirector`). |
| `internal/messaging/policy.go` | It writes to anyone, anyone writes to it. |
| `internal/work/service.go`, `feature.go` | Its ticket privileges; feature proposals (`ProposeFeature`, `StartProposedFeature`, `RefuseFeature`). |
| `internal/officemcp/director.go` | Its office tools, exposed at its address only. |
| `internal/daemon/director.go`, `proposals.go` | The tools' backing (staffing, overview); proposals and decisions as messages. |
| `internal/catalog/director/god.md` | Its built-in definition (`catalog.Director`), outside the catalog. |
| `web/src/components/chat/` (`cards.tsx`) | Its chat; proposals with Start / Refuse. |
