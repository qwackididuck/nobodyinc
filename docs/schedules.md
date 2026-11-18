# Scheduled tasks

A scheduled task is recurring work the daemon hands to an agent: "every 10 minutes, look at the production dashboard", "every weekday at 9, write the report". It runs **only while the daemon runs**: an occurrence that falls while the daemon is stopped is lost, never caught up. Decisions and rationale: ADR-0012.

There is no special kind of agent. A "background agent" is an ordinary agent, often in a dedicated team (`ops`, `watch`…), with a permission profile that lets it work unattended.

## The file

Schedules live in the project's `.nobody/schedules.yaml`, committed with it. Only the daemon writes it: the control room, the API and the director's accepted proposals go through it. Agents cannot write it (deny rules on `.nobody/`, ADR-0004).

```yaml
schedules:
  - id: grafana-watch            # lowercase letters, digits, dashes; unique
    title: Watch the production dashboard
    every: 10m                   # either every…
    # cron: "0 9 * * 1-5"        # …or cron, never both
    target: monitor@ops          # an agent, "@ops" (its first free member), or the director
    mode: message                # message | ticket
    enabled: true                # false: paused, or disabled by the daemon
    instructions: |
      Open the dashboard with the Grafana MCP server. If an alert is red,
      write to human with what you see; otherwise do nothing.
```

- A file edited by hand is read when the daemon starts, and before every change made through Nobody Inc, so a hand edit is kept by the next change. A missing `enabled` means enabled. An invalid file stops the daemon from starting, as an invalid `org.yaml` does.
- Changing a schedule restarts no agent and sends no org-change notice: the file is not part of the organization.
- What the schedules did is kept in the local database (`schedule_runs`), never in the file.

## When a schedule falls due

| Field | Meaning |
|---|---|
| `every` | A Go duration of one minute or more (`10m`, `2h`, `1h30m`). It counts from when the schedule is **armed**: the daemon starts, or the schedule is created, resumed or edited. |
| `cron` | Five standard fields (minute, hour, day of the month, month, weekday) or a descriptor (`@hourly`, `@daily`, `@weekly`), parsed by `robfig/cron/v3`. `TZ=`/`CRON_TZ=` prefixes and `@every` are refused: the time zone is the machine's, and `every` is the field for an interval. |

Times are read in **the daemon machine's time zone** (`time.Local`; UTC on a machine without tzdata). Around daylight saving changes (checked for Europe/Paris, `schedule/schedule_test.go`):

- a time that does not exist that day is **skipped**: `30 2 * * *` does not run on the day clocks go forward;
- a time that happens twice runs **twice**: `30 2 * * *` runs at 02:30 summer time and again at 02:30 winter time on the day clocks go back.

If the machine sleeps through several occurrences, one is due when it wakes up, not all of them.

## What an occurrence does

When a schedule falls due (`internal/daemon/schedules.go`):

1. **The previous occurrence is not over** (waiting or running): this one is **skipped**.
2. **The target is gone**: no agent can ever take it (the agent was dismissed, the team deleted or emptied, or the director is targeted in ticket mode). The schedule is **disabled** (`enabled: false`, written to the file) and the human is told.
3. **None of the target's agents runs** (on leave, stopped, exited, or the daemon was started with `-start=false`): the occurrence is **skipped and reported**. The human is told once per streak of such occurrences, not every ten minutes.
4. Otherwise it **waits** until a target agent is **free**, that is idle and with no ticket in progress, during a feature or not. It then goes to that agent; for a team target, the first member to be free gets it.

Every occurrence is signed by **`nobody`**:

| Mode | The agent gets | Over when | Suits |
|---|---|---|---|
| `message` | mail from `nobody` in its normal tray, act `inform` ("no answer needed"), with the instructions | the agent is idle again after the mail was delivered | work every few minutes or hours: nothing shows on the Kanban |
| `ticket` | a ticket assigned to it, created by `nobody`, with the instructions as description | the ticket is done (`done`), or canceled or deleted (`canceled`) | daily or weekly work: traceable, on the Kanban |

- The mail is never urgent: an urgent question would be answered by a session fork without tools (`hooks.md`).
- Nobody can answer `nobody`, and a ticket created by `nobody` tells nobody when it is done. Results stay where the work left them, in the ticket and the history, and the agent writes to the human only when its instructions say so. Write instructions accordingly ("if an alert is red, write to human").
- **Scheduled work is outside any feature.** Its ticket belongs to no feature: claiming it syncs nothing, closing it delivers nothing to the feature branch, it never blocks finishing the feature and is not archived with it. The agent still works where it is, which is its team's worktree during a feature. A commit it makes for a scheduled ticket stays on the team's branch. Scheduled tasks are meant for work that delivers no code.
- An occurrence blocked on a permission request waits like any work: with the `default` profile, every sensitive action goes to the approval queue, also at night. The control room warns about it when the target uses that profile.
- No spending guard yet: costs are not measured (see "Costs and budgets" in the roadmap). In message mode, occurrences pile up in the agent's session until Claude Code compacts it.

### History

Each occurrence is a row of `schedule_runs`, kept after its schedule is deleted: the trigger (`schedule`, or `manual` for "Run now"), when it fell due, started and ended, the agent, the ticket or message, a note, and its status:

```
waiting ──► running ──► done
   │           └──────► canceled     (ticket canceled or deleted, agent dismissed)
   └──────► canceled                 (schedule paused, edited or deleted while waiting)
skipped      the previous occurrence was not over
unavailable  no target agent could take it
interrupted  the daemon stopped while it was waiting, or running in message mode
```

When the daemon starts, a ticket-mode occurrence that was running keeps following its ticket, which survived the restart. Others that were not over are `interrupted`: the agents start new sessions.

## In the control room

- **Schedules tab**: the director's proposals first (Accept, Edit…, Refuse…), then one row per schedule (recurrence, target, mode, next and last occurrence, Run now, Pause/Resume). A schedule whose last occurrence found no agent is shown in alert, with the reason. The tab's badge counts alerts and pending proposals.
- **A schedule's drawer**: details, the profile warning, Edit, Pause/Resume, Run now (refused while an occurrence is not over), Delete, and the history with links to the tickets.
- **The form** offers presets and shows the next three occurrences, computed by the daemon (`POST /api/schedules/preview`), while the human types; a mistake shows under the field.
- **The office clock** rings like an alarm clock when an occurrence is done (a visual animation only; none when animations are reduced). Its badge counts the occurrences over since the human last looked. Clicking it lists the latest ones, done or skipped; one opens its schedule.

## The director

The director reads the schedules (`schedule_list`) and proposes new ones (`schedule_propose`, same fields). It cannot create, edit, pause or delete a schedule: there is no tool for that. A proposal is stored in the local database and reaches the human as a `propose` message. The human accepts it as it is or edited (the chat card's "Edit…" opens it in the Schedules tab), or refuses it with a reason. The director gets `agree` or `refuse`, and only acceptance writes the file. See `god.md`.

## API

`/api/schedules` (list, create), `/api/schedules/{id}` (edit, delete), `/api/schedules/{id}/pause|resume|run`, `/api/schedules/preview`, `/api/schedule-runs`, `/api/schedule-proposals` and `/{id}/accept|refuse`; event `schedules.changed`. Details in `api/openapi.yaml`.

## Code

| Piece | Role |
|---|---|
| `internal/schedule` | A schedule and its checks, recurrences (`recurrence.go`), the file (`file.go`), its only writer (`book.go`), the clock (`clock.go`: real, or manual for tests), the scheduler (`scheduler.go`), occurrences and proposals (`run.go`). |
| `internal/daemon/schedules.go` | What an occurrence does: dispatch rules, waiting for a free agent, ends, restart. |
| `internal/daemon/scheduleoffice.go` | What the API and the director do: create, edit, pause, run, delete, proposals. |
| `internal/store/schedules.go` | Occurrences and proposals in SQLite (migration `0007`). |
| `internal/server/schedules.go`, `internal/officemcp/schedules.go` | The HTTP endpoints; the director's tools. |
| `web/src/components/schedules/`, `web/src/components/office/ClockPanel.tsx` | The Schedules tab, its drawer and form; the clock's panel. |
| `test/e2e/schedules_test.go` | Whole scenarios with a manual clock: message mode waiting for a busy agent, ticket mode during a feature, a proposal accepted edited, a dismissed target. |
