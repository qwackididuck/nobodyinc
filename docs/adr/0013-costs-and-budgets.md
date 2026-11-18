# ADR-0013 — Costs and budgets: measured from the statusLine, enforced at the end of a turn

- Status: proposed
- Date: 2026-11-18

## Context

Nothing measures what the agents spend. An agent stuck in a loop, a message-mode schedule piling occurrences up in one session (ADR-0012) or a director splitting a large feature can burn money unseen. The human wants to see what is spent and to put **optional budgets** on the work, as a guard against an agent that runs away.

What "spent" means differs from one user to the next: the maintainer reaches the API through a proxy, others will use an API key or a Pro/Max subscription. The one figure they all have is the one Claude Code computes: **an estimate at list price, in USD**. Its documentation says that for subscriptions this figure "is not relevant for billing". So a budget here is a guard expressed in list-price USD, not an invoice.

What Claude Code offers, checked in its documentation:

- `--max-budget-usd` stops a session at a cost, but only in print mode (`-p`). Our agents are interactive sessions in a PTY.
- The session transcripts (JSONL) hold usage, but their format is internal and changes without notice.
- OpenTelemetry exports cost and tokens per request, with the session id. It is the way Anthropic recommends, but the daemon would need an OTLP receiver.
- The **statusLine**: a command Claude Code runs whenever the session changes, with a JSON description of the session on stdin, `cost.total_cost_usd` included. It is declared in the settings, as the hooks are.

What the contract tests found (`test/contract/statusline_test.go`, Claude Code 2.1.327):

- the statusLine runs in a session driven through a PTY, as our agents are, with `--setting-sources project,local` and the statusLine declared in `--settings`. It runs once when the session is ready, with a cost of 0, then several times per turn, never between turns;
- its JSON carries `session_id` and `cost` = `{total_cost_usd, total_duration_ms, total_api_duration_ms, total_lines_added, total_lines_removed}`. `total_cost_usd` is the session's running total;
- **a session resumed with `--resume` keeps its session id, but its running total starts again from 0**, as a fresh session's does (and, per the documentation, after `/clear`);
- an urgent question's fork (`claude -p --resume … --fork-session`, `runtime.Forker`) is another process that the agent's statusLine never sees. Its `-p` JSON output reports its own `total_cost_usd`;
- what the statusLine command prints is shown at the bottom of the agent's terminal.

## Decision

### 1. The cost comes from the statusLine

Every agent gets a statusLine in its generated `--settings` (`runtime.agentSettings`), next to its hooks: `nobody-hook statusline`, the same relay binary in a second mode. It posts the JSON it receives to the daemon (`POST /internal/agents/{address}/statusline`) and **prints nothing**, so the agent's terminal is unchanged. Like the hooks, it reaches the daemon through `NOBODY_AGENT` and `NOBODY_DAEMON_URL`, and it never fails loudly: if the daemon is unreachable, the report is lost and the next one carries the running total anyway.

The fork's cost is read by `runtime.Forker` from the fork's own `-p` output.

Not used: `--max-budget-usd` (print mode only), the transcripts (unstable format), OpenTelemetry (an OTLP receiver in the daemon: later, if per-request detail is ever needed).

Structured data still comes only from hooks, the statusLine and OTel, never from the PTY's screen (`CLAUDE.md`).

### 2. Spend is recorded as dated deltas, attributed when it happens

The daemon receives running totals per session. It turns them into **spend records** in the local database (migration `0008`): for each report, the delta since the previous report of the same agent and session. A **new start** counts from 0: another session ID (a fresh session, `/clear`), or a new process, since a session resumed with `--resume` keeps its ID while its total starts again from 0; the supervisor tells the meter whenever it starts an agent. Within a running session the total only grows: a lower total is a report that arrived late, behind a newer one (several statusLine runs may post at once), and is ignored rather than taken as a new start, which would count the spend twice. Reports whose delta is 0 write nothing.

Each record is attributed when it is written, from the daemon's state at that moment:

- the agent and its team (none for the director);
- **its ticket in progress**, and so the ticket's feature, when it has one;
- the schedule occurrence it is running, when it runs one (`schedule_runs`), so the schedule's history shows what each occurrence cost;
- the calendar month, in the machine's time zone, as schedules are (ADR-0012).

Spend with no ticket in progress (answering a colleague, the welcome turn, a message-mode occurrence, the director talking with the human, reading a notice) counts for the agent, its team and the staff only, never for a ticket or a feature.

**A fork's cost goes to the asker**, not to the agent that answers: to the asker's ticket in progress (and its feature) if it has one, otherwise to the asker, its team and the staff. The `ticket` field of the question does not matter. When the human asks, the fork's cost counts for the **staff only**. Only a fork can be charged to the asker: an answer given in the answering agent's own session is part of that session's running total and cannot be told apart.

The link between an agent and its session, so far kept only in memory (`runtime.Statuses`), is therefore recorded with every spend record.

### 3. Five levels of budget, all optional

| Level | Set by | Stored | Reset |
|---|---|---|---|
| **Ticket** | its creator (human, director or agent), at creation | the ticket's row in the database | never |
| **Feature** | proposed by the director in `feature_propose`, set or changed by the human when starting the feature | the feature's row | never |
| **Agent** | the human, or the director with the human's agreement (point 8) | `.nobody/budgets.yaml` | monthly, if enabled; otherwise a cumulative cap |
| **Team** | same | `.nobody/budgets.yaml` | same |
| **Staff** (every agent, the director included) | same | `.nobody/budgets.yaml` | same |

- **Inside a feature that has a budget, every ticket needs one**, and the ticket service refuses a ticket whose budget exceeds what is left to allocate. What is left is the feature's budget minus the budgets of its tickets that are not canceled, minus what its canceled tickets spent (a canceled ticket gives back what it did not spend). The feature's total spend is a budget in its own right.
- After creation, a ticket's budget changes only through the human (`PATCH`) or an extension (point 6).
- The director has no agent budget: **it is never held for budget**. Its spend is measured and counts for the staff, so it can bring other agents to a hold; it keeps working.

`.nobody/budgets.yaml` is committed with the project and written by the daemon only, like `schedules.yaml` (ADR-0004, ADR-0012): the control room, the API and the director's accepted proposals go through a `budget.Book`. It is read at start-up and before every change, so a hand edit is kept; an invalid file stops the daemon from starting. Like schedules, it is not part of the organization: changing it restarts nobody.

```yaml
enabled: true            # false: no caps, holds, requests or budget fields; spend is still measured
threshold: 80            # percent of a budget at which its agents stop (default 80)
staff:
  limit_usd: 200
  monthly: true          # reset on the first day of each month; otherwise cumulative
teams:
  dev: { limit_usd: 80, monthly: true, threshold: 90 }
agents:
  alice@dev: { limit_usd: 30, monthly: true }
```

An entry whose agent or team no longer exists is removed at the next write.

### 4. The threshold, and what happens at it

Everything happens at **X % of a budget, 80 % by default**: one threshold for the project in `budgets.yaml`, which a budget may override (ticket and feature rows carry an optional threshold too). Nothing more happens at 100 %: the agents are already holding, and what a turn in progress spends past the limit is only shown.

When a spend record brings a budget to its threshold, the agents that budget covers are **held**:

1. An agent in the middle of a turn **finishes its turn**. At its `Stop`, the daemon refuses the stop **once** and asks it to call the new office tool `budget_request` (amount asked, justification: where it is, what remains), unless point 5 gives the justification to the director. An agent that stops again without calling it is held anyway; its request then carries no justification.
2. An idle agent is held at once, without being woken up.
3. A held agent is **on budget hold, its session kept**: the process stays alive and is never woken up; mail waits in its trays; it is offered no ticket (`pickMember`, claims refused like `checkFree`) and no schedule occurrence (`scheduledWork.freeAgent`: the occurrence waits). Its status is `budget_hold`, shown in the office with an empty-wallet icon. What the human types into its terminal still reaches it, and is still counted.

A hold is not stored as such: an agent is held while a budget that covers it is reached. Anything that takes a budget back under its threshold (an extension, a monthly reset, a budget raised or removed, budgets turned off) releases its agents by itself, and a hold survives a daemon restart since the spend and the budgets do. The daemon works the holds out again after every spend record, budget change and ticket or feature change, and at the start of every month. A held agent restarted (daemon restart, definition change) runs its first turn (onboarding or resume) and is held again at its end. A ticket whose budget, or its feature's, is reached cannot be claimed.

### 5. One request per budget; the director escalates, the human decides

There is **one request per budget**, whatever the number of agents it holds. An extension releases them all.

- For a **ticket, agent or team** budget, the **first agent to stop** writes the justification with `budget_request`. The others are held without being asked. The request then goes to the **director**, as mail, who passes it on to the human **with its opinion** (`budget_submit`: its opinion and the amount it recommends).
- For a **global** budget, **staff or feature**, no agent is asked: the **director writes the justification** with `budget_submit`, since it has the overall view.

The director grants nothing: it has no tool to change a budget. Its submission appears in **the director's chat** as a card for the human, with the request, the justification and the director's opinion, and an alert. There is no badge on the held agent's avatar.

The human decides on the card, and the code enforces it:

- **Extend +$X** (the amount asked, editable).
- **Refuse**, choosing the outcome for each held agent: **cancel** its ticket, **give the ticket back** to its team's queue, or **keep it on hold**. **"Commit the work in progress first"** is a box combined with that outcome: the agent is released for one short turn with a notice from `nobody` ("commit your work in progress, comment the ticket, then stop"), and the outcome applies at the end of that turn. What that turn spends is shown, never stopped. No outcome is imposed by default.

The agents and the director hear the decision as notices from `nobody`. A released agent is woken up with its memory intact.

If the director is not running when a request needs it, the request waits in its tray like any mail.

### 6. What an extension changes

- A **ticket**: its budget grows. Inside a feature with a budget, the extension comes out of what is left to allocate; when that is not enough, the card says so and extending raises the feature's budget by the difference.
- A **feature**: its budget grows.
- A **monthly** agent, team or staff cap: the extension holds **for the current month only** (recorded in the database); the cap in `budgets.yaml` does not change.
- A **cumulative** cap: the new cap is written to `budgets.yaml`.

### 7. A monthly reset lifts a hold

When a month begins, monthly caps start again from 0. Agents held **only** by a monthly cap are released, and its pending request is closed as "monthly reset"; the director and the human are told. A cumulative cap never resets: its agents stay on hold until the human decides.

### 8. The director proposes caps, the human decides

The director gets `budget_list` (the caps, the spend per level and per month) and `budget_propose` (an agent, team or staff cap: amount, monthly, threshold). A proposal works like `schedule_propose` (ADR-0012): stored in the local database, sent to the human as a `propose` message, accepted (possibly edited) or refused from its card; the director gets `agree` or `refuse`. Only acceptance writes `budgets.yaml`.

`feature_propose` gets an optional budget. The human sees it, changes it or accepts it when starting the feature. `ticket_create` gets a budget field, mandatory inside a feature that has a budget, checked as in point 3.

### 9. Turning budgets off

`enabled: false` in `budgets.yaml` (Settings) turns budgets off for the project: no caps, thresholds, holds or requests, and no budget field in the forms or tools. **Spend is still measured and shown.** Holds and pending requests are lifted when budgets are turned off.

### 10. What the human sees

Spend reaches the control room through the REST API (`/api/spend`, `/api/budgets`, `/api/budget-requests`, `/api/budget-proposals`) and SSE events (`spend.changed`, sent at most once a second; `budgets.changed`). The interface design was decided with the human (roadmap, "Costs and budgets"): gauges on the agents' desks and in the team rooms, a new spending board in the office opening the spending popup (with the month's total, and each schedule's cost for the month), spent/budget on tickets and the feature banner, each occurrence's cost in a schedule's history, cap fields in Staff, the team dialog and Settings.

## Not decided here

- OpenTelemetry as a second, per-request source.
- Spending caps per schedule occurrence, or a maximum duration.
- Real billing (subscription quotas, actual invoices).
- Budgets for the director, which this ADR leaves without a cap.

## Consequences

- The agents' cost becomes visible, per agent, team, ticket, feature, schedule and month, whatever the user pays for Claude Code.
- `nobody-hook` gets a second mode and the agents a statusLine; nothing changes in what agents see.
- A new agent status, `budget_hold`, which wake-ups, ticket picking and schedules respect.
- The figure is Claude Code's estimate at list price: it can drift from what a provider bills, and it ignores what a subscription covers.
- Spend is lost between a report and the next only if the daemon is down; a statusLine report lost while the daemon runs is caught up by the next running total, except at the end of a session.
- Tickets and features get budget columns; new tables for spend records (migration `0008`), then holds, requests and proposals (later migrations); a new file `.nobody/budgets.yaml`. No new dependency.
- Behaviours checked against the real CLI: `test/contract/statusline_test.go`. Rerun after every Claude Code upgrade.
