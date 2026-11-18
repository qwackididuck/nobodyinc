# ADR-0009 — Flows between teams: open by default, restricted by a block list

- Status: accepted
- Date: 2026-11-17
- Replaces: the one-way channels of ADR-0004 (`channels:` in `.nobody/org.yaml`)

## Context

Until now, an agent could only start a conversation with its own team, the human, the director, and the teams its team had a **channel** to: one-way pairs declared in `.nobody/org.yaml`. Channels also decided which teams an agent could create tickets for.

Since teams are created from the control room and by the director (ADR-0008 era), a new team has no channel. Its agents cannot ask another team anything, although the question routing rule tells them to ask whoever owns the subject: the product analyst for the specification, the architect for the design, often in another team. In practice those questions ended with the human or the director, which is what the rule is meant to avoid. Declaring every useful pair by hand does not scale with teams created on the fly.

## Decision

1. **Open by default.** Any agent may start a conversation with any agent of any team.
2. **Restrictions are a block list per team.** A team lists the teams its members may not start a conversation with: `block: [qa]` under the team in `org.yaml`. A block is one-way: `dev` blocking `qa` says nothing about `qa` writing to `dev`.
3. **A block only stops starting a conversation**, including the team address (`@qa`). A blocked agent can always answer a message it received. The human, `nobody` and the director are never blocked, in either direction.
4. **Tickets are open.** Any agent may create a ticket for any team; blocks do not apply to tickets.
5. **Channels are removed.** `channels:` is no longer part of `org.yaml`. A project that still has one loads with a warning, the entries are ignored, and the next write of `org.yaml` drops them.
6. **Who edits it:** the human, in the Staff tab of the control room (which replaces Recruitment), while editing a team; the director, through an office tool, only with the human's agreement (as `team_delete`); and the API (`PUT /api/teams/{team}`).
7. **Running agents are told, not restarted.** A change of blocks reaches the agents concerned as an org-change notice from `nobody`; their sessions go on. `list_contacts` always shows the current state; a send refused by a block says so and tells the agent to ask the human instead.
8. **One asker per team.** Before sending a question to another team, an agent checks with its own teammates: one of them may know the answer, or may already have asked it. This avoids the same question reaching a team several times. It is part of the routing rule in the protocol prompt.

## Not decided here: renaming a team

Renaming a team stays out of scope. A team id is part of every member's address (`alice@dev`), of the files in `.nobody/teams/<id>/`, of the team's worktree and branch during a feature (`nobody/<feature>--<id>`), of its tickets, and of the message history. A rename would have to move or rewrite all of them consistently, or keep an alias forever. It needs its own decision.

## Consequences

- Agents reach whoever owns the subject without a hand-made channel; the human only sees the questions the routing rule sends them.
- The organization chart in the system prompt and `list_contacts` show, per team, whether the agent may start a conversation with it or only answer (blocked).
- `messaging.ErrNoChannel` becomes a block refusal; `work` no longer checks flows for tickets.
- Docs that describe channels are rewritten (`docs/architecture.md`, `docs/agents.md`, `docs/god.md`, `docs/tickets.md`); ADR-0004's mention of channels is superseded by this record.
