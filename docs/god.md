# The director ("god")

The director talks with you, turns your requests into features and tickets, and leads the teams. It is an agent like the others, with no team and more rights. Decisions and rationale: ADR-0005.

## Setting it up

There is nothing to do: the director is always present. It is not in the catalog. Setting a folder up as a project writes it, alone (you then build the teams, or ask it to; `projects.md`), and the project's daemon writes it when it starts on a project that has none (`org.EnsureDirector`, from `internal/catalog/director/god.md`).

Its file is `.nobody/director/<name>.md`, and its address is that name: rename the file `boss.md` and everyone writes to `boss`. There is exactly one. Edit it like any agent (Staff tab); it can be neither put on leave nor dismissed. Its built-in definition uses Opus and the `default` permission profile.

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

1. **The need.** It clarifies your request until the need and its acceptance criteria are clear and testable, reading the project and the documentation MCP servers every agent gets (the project's `.mcp.json` and your own Claude Code servers, `docs/agents.md`, "MCP servers").
2. **The proposal.** It proposes a feature with `feature_propose`. Nothing happens yet: the proposal shows in its chat, with **Start** and **Refuse**, and in the banner above the office.
   - **Start** creates the feature branch and the teams' worktrees, makes it the active feature, restarts the team agents there (not the director, which keeps the conversation), and answers `agree` to the director.
   - **Refuse** answers `refuse` with your reason; it can work on it with you and propose again.
3. **The work.** It splits the feature into tickets for the teams (dependencies, acceptance criteria, assignee when obvious), follows them with `org_overview`, unblocks and arbitrates, and brings you the decisions that are yours.

## What it may do

| | Team agent | Director |
|---|---|---|
| Write to | any team except those its team blocks (ADR-0009), the human, the director; answer anyone | anyone, never blocked |
| Create tickets for | any team | any team, with an assignee |
| Change a ticket's status | its own tickets | any ticket |
| Propose a feature | no | yes (`feature_propose`); you start or refuse it |
| Clean the Kanban | no | yes (`board_clean`, only with your agreement) |
| Close the open conversations between agents | no | yes (`conversations_close`, only with your agreement) |
| Create and delete teams, restrict their flows | no | yes (`team_create`; `team_delete` and `team_block` only with your agreement) |
| Recruit, create, edit, put on leave, dismiss agents | no | yes, freely; never itself |
| Scheduled tasks | no | reads them and proposes new ones (`schedule_propose`); you accept, edited or not, or refuse |
| Budgets | justifies an extension with `budget_request` when a budget holds it | never held; passes extension requests on to you with its opinion (`budget_submit`), justifies the staff's and features' itself; proposes caps (`budget_propose`); grants nothing |

Its own tools, on top of the shared ones (`send_message`, `list_contacts`, `ticket_*`):

| Tool | Does |
|---|---|
| `feature_propose` | Proposes a feature (title, need, acceptance criteria, base branch, and optionally a budget, which you change or keep when starting it). |
| `org_overview` | The active feature, and for each team its goal and prompt, its agents (role, expertise, status, on leave) and tickets (open, in progress, done and canceled counts). |
| `team_create` | Creates a team (id, goal, and the prompt its members share, which it writes from the goal). |
| `team_delete` | Deletes a team and dismisses its members; its instructions ask it to get your agreement first. |
| `team_block` | Replaces a team's block list: the teams its members may not start a conversation with (`docs/agents.md`, "`org.yaml`"). Nobody restarts; both teams get a notice. Its instructions ask it to get your agreement first. |
| `template_list`, `agent_get` | The catalog; an agent's definition. |
| `agent_recruit`, `agent_create` | Recruit from a template, or create from a definition written from scratch, into a team. |
| `agent_edit` | Replace an agent's definition (a new role or new instructions restart it with a fresh session). |
| `board_clean` | Deletes the Kanban's tickets for good: `closed` (done and canceled) or `all`. Only with your agreement (`docs/tickets.md`, "Cleaning the board"). |
| `conversations_close` | Closes for good every open conversation between agents (a request, query or proposal nobody answered); the agents that waited are told. Never touches conversations with you. Only with your agreement (`docs/architecture.md`, "Closing open conversations"). |
| `agent_leave`, `agent_return`, `agent_dismiss` | As in the Staff tab (`docs/agents.md`, "Lifecycle"). |
| `schedule_list` | The scheduled tasks, with their next and last occurrence (`docs/schedules.md`). |
| `schedule_propose` | Proposes a scheduled task. Nothing runs until you accept it, as it is or edited (its card in the chat: Accept, Edit…, Refuse…); it gets `agree` or `refuse`. It has no tool to create, edit, pause or delete one itself. |
| `budget_list` | The budgets: settings, this month's spend, every gauge, the open extension requests (`docs/budgets.md`). |
| `budget_submit` | Passes an extension request on to you, with its opinion and the amount it recommends; for the staff's or a feature's budget, or a request no agent justified, with its own justification. Its card in the chat: Extend +$X / Refuse…. |
| `budget_propose` | Proposes an agent, team or staff cap; you accept it, possibly with another limit, or refuse it. |
| `decision_log` | In Nobody mode only (`automation.md`): records a decision taken in your place on an agent's question, with its rationale and impact, and sends it to the agent. |

It is not a source of knowledge about the project: it knows what you want, the teams know how the project works. The agents' routing rule treats a question to it like a question to you (`docs/agents.md`, "Whom to ask"), and its own prompt asks it to send questions about the project to the agent whose expertise covers them.

Changes to agents are written by the daemon (`internal/staffing`), as when you make them; the director cannot change its own definition, leave or dismiss itself. When a change is its own idea, its instructions ask it to say so and, before creating an agent from scratch, to show you the whole definition.

## Code

| Piece | Role |
|---|---|
| `internal/org/director.go` | The director in the organization: its file, its address, exactly one (`EnsureDirector`). |
| `internal/messaging/policy.go` | It writes to anyone, anyone writes to it. |
| `internal/work/service.go`, `feature.go` | Its ticket privileges; feature proposals (`ProposeFeature`, `StartProposedFeature`, `RefuseFeature`). |
| `internal/officemcp/director.go`, `schedules.go` | Its office tools, exposed at its address only. |
| `internal/officemcp/decisions.go`, `internal/daemon/nobody.go` | `decision_log`: its decisions in the human's place in Nobody mode (`automation.md`). |
| `internal/daemon/scheduleoffice.go` | Its schedule proposals and the human's decisions, as messages (ADR-0012). |
| `internal/officemcp/budgets.go`, `internal/daemon/budgetoffice.go`, `budgetproposals.go` | Its budget tools; extension requests and cap proposals, as messages (ADR-0013). |
| `internal/daemon/director.go`, `proposals.go` | The tools' backing (staffing, overview); proposals and decisions as messages. |
| `internal/catalog/director/god.md` | Its built-in definition (`catalog.Director`), outside the catalog. |
| `web/src/components/chat/` (`cards.tsx`) | Its chat; feature proposals with Start / Refuse, schedule proposals with Accept / Edit… / Refuse, budget requests with Extend / Refuse, cap proposals with Accept / Refuse. |
