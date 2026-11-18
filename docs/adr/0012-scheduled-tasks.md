# ADR-0012 — Scheduled tasks: the daemon hands recurring work to ordinary agents

- Status: proposed
- Date: 2026-11-18

## Context

The human wants work that comes back on its own: watching a Grafana dashboard every 10 minutes, a daily report, checking dependencies every Monday, reviewing blocked tickets. Each is handed to an agent whose mission it is.

Claude Code has a scheduler of its own (`CronCreate`, `/loop`), but it lives inside a session and dies with it. Our agents are restarted when their definition changes, when a feature starts or ends, and whenever the daemon stops. So the daemon has to do the scheduling.

Most of what this needs already exists:

- mail wakes an idle agent up (`Supervisor.NotifyMail`);
- a ticket assigned to an agent reaches it as a notice from `nobody` (`docs/tickets.md`);
- `nobody` may write to anyone, and nobody may write back to it (`messaging/policy.go`). A ticket created by `nobody` tells nobody when it is done (`daemon/notify.go`);
- the director's feature proposals show how to let it suggest a change that only the human may decide on (`feature_propose`, ADR-0005).

Two things do not exist yet. The daemon has no timer (only the SSE heartbeat), and there is no cron library in `go.mod`. Also, the ticket service and the post office each report their changes to a single subscriber (`work.Service.OnEvent`, `messaging.Service.OnSent`). The same goes for `runtime.Statuses.OnIdle`.

## Decision

### 1. A schedule is committed with the project

Schedules are declared in `<project>/.nobody/schedules.yaml`, next to `org.yaml`, and travel with the repository (ADR-0004). Only the daemon writes the file: the control room and the API go through it, agents keep their deny rules on `.nobody/`, and the director only proposes (point 9). The file is read at start-up. A change made by hand is picked up at the next change made through Nobody Inc, or when the daemon restarts, as for agent files. The file does not belong to the organization (`org.Org`): changing a schedule restarts nobody and sends no org-change notice.

```yaml
schedules:
  - id: grafana-watch            # ^[a-z0-9][a-z0-9-]{0,63}$, unique
    title: Watch the production dashboard
    every: 10m                   # either every…
    # cron: "0 9 * * 1-5"        # …or cron, never both
    target: monitor@ops          # an agent, "@ops" (any member), or the director
    mode: message                # message | ticket
    enabled: true                # false: paused, or disabled by the daemon
    instructions: |
      Open the dashboard with the Grafana MCP server. If an alert is red,
      write to human with what you see; otherwise do nothing.
```

What each schedule did lives in the local database, never in the file (point 8).

### 2. Recurrence: an interval or a cron expression

- `every`: a Go duration of one minute or more. The first occurrence comes one interval after the schedule is armed (the daemon starts, or the schedule is created, resumed or edited), then once per interval.
- `cron`: a standard five-field expression, or a descriptor such as `@daily`, parsed by `robfig/cron/v3` (`cron.ParseStandard`; new dependency, accepted by the human).
- Times are read in **the machine's time zone** (`time.Local`). Around daylight saving changes, we keep the library's behaviour, pinned by a test. On a machine without tzdata (a bare container), the local zone is UTC.

### 3. Only while the daemon runs, no catch-up

An occurrence that falls while the daemon is stopped is lost: no catch-up at restart. A schedule only means "while Nobody Inc runs". When the daemon starts, it arms every enabled schedule from now. An occurrence that was waiting or running in message mode when the daemon stopped is recorded as `interrupted`. A ticket-mode occurrence keeps following its ticket, which survives the restart.

### 4. Two modes, chosen per schedule

The human picks the mode when creating the schedule, depending on its frequency:

- **`ticket`**: each occurrence creates a ticket assigned to the chosen agent, titled after the schedule, with the instructions as its description. The work is traceable (deliverables, comments) and shows on the Kanban. This suits a daily or weekly task. The occurrence is over when the ticket is **done**, or **canceled** or deleted.
- **`message`**: each occurrence sends the instructions to the chosen agent as mail. Nothing shows on the Kanban, which suits a task every few minutes: 144 tickets a day would drown the board. The occurrence is over when **the agent becomes idle again** after the mail.

A ticket-mode schedule needs a team agent, or a team, as its target: the director has no team, and so no tickets.

### 5. Out of any feature

Scheduled work does not belong to the active feature:

- its ticket gets no feature (`work.Draft` gains a way to say so). Claiming it does not sync the worktree, closing it delivers nothing to the feature branch, it never blocks "Finish" and is not archived with the feature;
- the occurrence simply waits until an agent able to take it is free (point 6), during a feature or not.

The agent still works in the directory it is in: its team's worktree during a feature, otherwise the project. Scheduled tasks are meant for work that delivers no code. A commit made during one stays on the team's branch.

### 6. Dispatch: as soon as a target agent is free

When an occurrence falls due:

1. **Overlap.** If the schedule's previous occurrence is still waiting or running, the new one is **skipped** (recorded as `skipped`).
2. **Target gone.** If no agent can ever take it, the daemon **disables the schedule** (`enabled: false`, written to the file) and tells the human (point 7). A target is gone when the agent was dismissed, its team was deleted, the team has no members left, or the director is targeted in ticket mode.
3. **Target unavailable.** The occurrence is **skipped and reported** when the target exists but none of its agents runs: on leave, stopped, exited, or the daemon started with `-start=false`. Only the first occurrence of a run of unavailable ones is reported, so the human does not get a notice every ten minutes.
4. **Otherwise it waits** (`waiting`) until a target agent is **free**: idle, and with no ticket in progress (one thing at a time, as for tickets: `work.Service.checkFree`). It is then dispatched to that agent; for a team target, the first member to be free gets it.

Scheduled work is signed by **`nobody`**:

- the ticket is created by `nobody`, so its completion tells nobody;
- the mail comes from `nobody`, in the normal tray, with the act `inform` ("no answer needed"). Never urgent: an urgent question would be answered by a session fork without tools (`docs/hooks.md`).

Results stay where the work left them: in the ticket, and in the schedule's history. The agent writes to the human only when the instructions ask it to ("if an alert is red, write to human").

### 7. The human is told in the chat

A schedule disabled because its target is gone, or an unavailable target, reaches the human as a notice from `nobody` (act `inform`). It names the schedule and says what to do: recruit an agent, change the target, bring an agent back from leave.

### 8. History in the local database

Every occurrence is a row, kept even after its schedule is deleted (migration `0007`, table `schedule_runs`). Each row records:

- the schedule's id and title;
- why it ran: the schedule itself, "Run now", or a test;
- when it fell due, started and ended;
- its status: `waiting`, `running`, `done`, `canceled`, `skipped`, `unavailable`, `interrupted`;
- the agent it went to, and its ticket or message;
- a note, e.g. why it was skipped.

An occurrence blocked on a permission request shows as such, because its agent's status is `awaiting_approval`. Changes reach the control room with a new SSE event, `schedules.changed`.

### 9. The director proposes, the human decides, enforced by the code

The director gets one office tool, `schedule_propose`, with the same fields as a schedule. It cannot create, edit, pause or delete a schedule itself. Like `feature_propose`:

- the proposal is stored in the local database (`schedule_proposals`), not in `schedules.yaml`;
- the human receives it as a `propose` message from the director;
- in the control room, the human **accepts** it (possibly after editing it) or **refuses** it, with an optional reason;
- the director receives the decision as `agree` or `refuse` mail.

Only acceptance writes the schedule to the file. The director can also read the schedules (`schedule_list`), so it does not propose one that already exists.

### 10. What the human does

The human uses a new **Schedules** tab of the control room, backed by `/api/schedules*`:

- list the schedules with their next occurrence and their history;
- create and **edit** a schedule;
- **pause / resume** it;
- **Run now**: one occurrence at once, following the dispatch rules of point 6. It is refused while an occurrence is waiting or running;
- **delete** it. Its history stays in the database;
- accept or refuse the director's proposals.

Some details:

- Pausing or editing a schedule cancels an occurrence that is still waiting. An occurrence that is already running goes on to its end.
- An edit re-arms the recurrence.
- At creation and edit, when the target agent uses the `default` permission profile, the control room warns the human: every sensitive action will wait in the approval queue.
- **No spending guard in V1.** Costs are not measured yet; budgets come with "Budgets and observability".

## Not decided here

- A fresh session per occurrence, instead of mail to a long-lived session (hooks, office MCP and `PermissionRequest` in that mode would need contract tests).
- Spending caps and maximum duration per occurrence (with budgets).
- Retrying a failed occurrence.
- The director editing, pausing or deleting schedules.
- Several daemons on the same project: each machine runs the committed schedules on its own.

## Consequences

- Recurring work is a schedule in a file, carried out by ordinary agents through tickets and mail. No new kind of agent, and no new message protocol.
- A "background agent" is simply an agent of a dedicated team (`ops`, `watch`…), with the permission profile its work needs.
- The daemon gets its first timer: one scheduler goroutine with an injectable clock, so tests never wait.
- `work.Service.OnEvent` and `runtime.Statuses.OnIdle` get a second subscriber, so they turn into fan-outs. `messaging.Service.OnSent` is not needed. Canceling a ticket now emits an event (`work.EventCanceled`), since it ends a ticket-mode occurrence.
- New dependency: `github.com/robfig/cron/v3`.
- Message mode piles occurrences up in one long session: its context grows until Claude Code compacts it. An agent idle for a long time is woken up without a warm prompt cache. Both cost money that nothing measures yet.
- Two developers running Nobody Inc on the same project both run its schedules.
