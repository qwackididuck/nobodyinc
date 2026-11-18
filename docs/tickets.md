# Tickets

A ticket is a unit of work the organisation tracks. It belongs to a **team**; while it has no assignee it waits in that team's **queue**, and any member can claim it. Messages, comments and deliverables attach to it, so a ticket tells the whole story of a piece of work.

## Life of a ticket

```
open ──claim──► in_progress ──done──► done
  ▲                 │
  └────release──────┘        (any open state) ──cancel──► canceled
```

- A ticket can depend on other tickets (`blocked_by`): it cannot be claimed until they are all done.
- An agent works on **one ticket at a time**: claiming a second one is refused until the first is done or released.
- A ticket belongs to the active **feature**, if any: claiming and closing it then move work through git (see `workspaces.md`), and the delivered commits become `commit` deliverables. The exception is the ticket of a **scheduled task** (`schedules.md`): created by `nobody` and assigned to a free agent, it belongs to no feature, so it moves nothing through git, never blocks finishing the feature and is not archived with it.
- IDs look like `T-12`.

## Who may do what

| Action | Allowed for |
|---|---|
| Create | Anyone, for any team: the human, the director (`docs/god.md`, who may also assign it) and every agent. A team's block list only limits who its members start conversations with, never tickets (ADR-0009). |
| Claim | A member of the owning team, once the ticket is open and unblocked. |
| Done / release / cancel | The assignee, the human or the director. |
| Comment, attach a deliverable | Anyone in the organisation. |

Refusals come back to the agent as readable tool errors that say what to do instead.

## How agents hear about work

Ticket events become notices from `nobody` (`internal/daemon/notify.go`), which wake idle agents up like any mail:

| Event | Who is told |
|---|---|
| New ticket in a team's queue | **One** member of that team, preferring an idle one, so a whole team is not woken up for one piece of work. Blocked tickets are announced only once unblocked. |
| Ticket created with an assignee | The assignee. |
| Ticket released to the queue | Another available member of the team. |
| Ticket done | Its creator (unless it is `nobody`, as for a scheduled task's ticket), and one member of each team whose ticket it was blocking. |

## Agent tools

On the office MCP server (`internal/officemcp/tickets.go`):

| Tool | Purpose |
|---|---|
| `ticket_list` | My tickets in progress and my team's queue. |
| `ticket_get` | A ticket with its description, acceptance criteria, deliverables and comments. |
| `ticket_claim` | Take a ticket (or, without an id, the oldest available one). |
| `ticket_create` | Create a ticket (optionally assigned, optionally blocked by others). |
| `ticket_update` | Done, release (`open`) or cancel, with an optional comment. |
| `ticket_comment` | Progress, questions, decisions. |
| `deliverable_add` | A file, a commit, a report or a note. |

`send_message` has a `ticket` field; an answer inherits the ticket of the message it answers, so a whole thread stays attached.

## Team addresses

A message to `@<team>` (e.g. `@specs`) goes to one member of that team, chosen the same way as for queued tickets: idle first, then any running member. Use it when any member can answer.

Agents on leave are never chosen, neither for a queued ticket nor for `@<team>`. When an agent goes on leave or is dismissed, its ticket in progress goes back to its team's queue once it has stopped, with a comment from `nobody` (`docs/agents.md`, "Lifecycle").

## API and UI

`/api/tickets*` (see `api/openapi.yaml`) lets the human list, create, comment and change the status of tickets. The control room has a **Kanban** tab: tickets as cards in one column per status (dragging a card changes its status), a creation form, and a drawer with a ticket's deliverables, comments and messages.

**Deleting a ticket** (`DELETE /api/tickets/{id}`): the human drags its card onto the Kanban's shredder. The ticket, its comments and its deliverables are deleted for good; messages about it stay, without the link. If an agent was working on it, it gets an urgent notice from `nobody` to stop. With `cascade=true` (a box in the shredder's confirmation), the tickets that depend on it, and theirs, are deleted too; otherwise they no longer wait for it.

**Archived tickets**: finishing a feature archives its tickets (ADR-0007). They leave the Kanban and every queue, and stay readable: `GET /api/tickets?archived=true`, and the archives room of the office. They cannot be deleted.

## Not yet

- Bids (calls for tender) and the god session that writes the tickets from the requirements.
