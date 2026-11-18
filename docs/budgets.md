# Costs and budgets

Nobody Inc measures what every agent spends and holds the agents to optional budgets. Decisions and rationale: ADR-0013.

**What the amounts are.** Every amount is Claude Code's own estimate of a session's cost, at list price, in USD. It is the one figure every user has, whether they pay through an API key, a proxy or a Pro/Max subscription (for which Claude Code says the cost "is not relevant for billing"). So a budget is a guard against an agent that runs away, not an invoice.

## Measuring

```
Agent (claude)          nobody-hook statusline              nobodyd
     │ the session changes        │                            │
     ├─ statusLine JSON (stdin) ─►│── POST …/statusline ──────►│ delta since the last report
     │◄─ (prints nothing) ────────│                            │ → a spend record, attributed
```

- Every agent's generated `--settings` declares a statusLine: `nobody-hook statusline` (`runtime.agentSettings`). Claude Code runs it once when the session is ready, then several times per turn, with a JSON description of the session that includes `session_id` and `cost.total_cost_usd`, the session's running total. The relay posts it to the daemon (`POST /internal/agents/{address}/statusline`), prints nothing, so the agent's terminal is unchanged, and always exits 0.
- The daemon (`budget.Meter`) turns running totals into **spend records**, each the delta since the previous report of the same agent and session (table `spend`, migration `0008`):
  - another session ID starts from 0;
  - so does a new process: the supervisor tells the meter whenever it starts an agent (`Supervisor.OnStart`), because a session resumed with `--resume` keeps its ID while its total starts again from 0;
  - within a session the total only grows: a lower total is a late report, ignored (several statusLine runs may post at once).
- An urgent question's **fork** (`runtime.Forker`) is another process, which the agent's statusLine never sees. Its cost comes from its own `-p` output (`total_cost_usd`) and goes to the **asker**: its ticket in progress, or the asker itself, its team and the staff; when the human asked, the staff only.
- What Claude Code shows is checked against the real CLI by `test/contract/statusline_test.go`. Rerun it after every Claude Code upgrade.

### Attribution

Each record is attributed when it is written, from the daemon's state at that moment (`daemon.spendAttribution`):

| Counts for | From |
|---|---|
| the agent and its team | its address (the director has no team) |
| a ticket and its feature | the agent's ticket in progress, if any |
| a schedule occurrence | the occurrence the agent runs, if any (`schedule_runs`) |
| a month | the record's time, in the machine's time zone |
| the staff | every record |

Spend with no ticket in progress (answering a colleague, the welcome turn, a message-mode occurrence, the director talking with the human) counts for the agent, its team and the staff only. Tickets, features and occurrences show what they spent (`spent_usd`, computed by the store).

## Budgets

| Level | Set by | Stored | Reset |
|---|---|---|---|
| Ticket | its creator (human, director, agent), at creation; then the human (`PATCH /api/tickets/{id}`) | with the ticket (migration `0009`) | never |
| Feature | proposed by the director (`feature_propose`), set by the human when starting it | with the feature | never |
| Agent, team, staff | the human, or the director's proposals once accepted | `.nobody/budgets.yaml` | monthly if `monthly`, otherwise cumulative |

- **Inside a feature with a budget**, every ticket needs one, at most what the feature has **left to allocate**: its budget minus what its tickets take (`allocated_usd`): each ticket that is not canceled takes its budget, or what it spent when that is more; everything else spent on the feature (canceled and deleted tickets) is gone for good.
- **The director** has no cap of its own and is never held. Its spend counts for the staff.
- **Thresholds**: everything happens at a percent of a budget, 80 % by default, set for the project in `budgets.yaml` and overridable by any budget.
- **Turning budgets off** (`enabled: false`, Settings → Budgets) drops caps, thresholds, holds, requests and budget fields. Spend is still measured and shown.

```yaml
# .nobody/budgets.yaml, written by the daemon only (budget.Book), committed
enabled: true
threshold: 80
staff:
  limit_usd: 200
  monthly: true
teams:
  dev: { limit_usd: 80, monthly: true, threshold: 90 }
agents:
  alice@dev: { limit_usd: 30 }
```

A missing file means budgets on, 80 %, no cap. An invalid file stops the daemon from starting. A file edited by hand is read before every change, and a cap of a team or agent that no longer exists is dropped at the next write.

## At the threshold

A **hold** is not stored: an agent is held while a budget that covers it is reached. A budget covers its ticket's assignee, the agents working on a feature's tickets, an agent, a team's members, or every agent for the staff (`budget.Covered`); never the director. So anything that takes a budget back under its threshold (an extension, a monthly reset, a budget raised or removed, budgets turned off) releases its agents by itself. The daemon works the holds out again after every spend record, budget change, ticket or feature change, and at the start of every month (`daemon.budgetGuard`).

1. **The turn ends.** An agent in the middle of a turn finishes it; one idle is held at once, without being woken up.
2. **One request per budget** (`BR-<n>`), active while the budget stays reached:
   - for a ticket, agent or team budget, the **first agent to stop** has its `Stop` refused once and is asked to call `budget_request` (amount, justification). If it stops again without answering, or if nobody was in a turn to be asked, the request goes to the director unjustified;
   - for the **staff** or a **feature** budget, the **director** justifies it: it has the overall view.
3. **On hold**: status `budget_hold`, session kept, never woken up, mail waiting, no ticket offered or claimable (`work.Service.CheckClaim`), no schedule occurrence. A ticket whose budget, or its feature's, is reached cannot be claimed. A held agent restarted (definition change, fresh session, or a turn interrupted when the project closed) runs its first turn, then is held again; one resumed silently when the project opens again (ADR-0015) is put on hold at its `SessionStart`, without a turn.
4. **The director** gets the request as a notice from `nobody` and passes it on with `budget_submit`: its opinion and the amount it recommends (and the justification when nobody wrote one). The human gets a `propose` message from the director, shown as a card in its chat, and a badge on its avatar. The director grants nothing.
5. **The human decides**, from the card:
   - **Extend +$X**: a ticket's budget grows (out of what its feature has left, and the feature grows by what is missing); a feature's grows; a monthly cap gets an extension for the current month only (table `budget_extensions`); a cumulative cap is written to `budgets.yaml`. An extension that would leave the budget at its threshold is refused with the minimum. The held agents get a notice and start again with their memory; the director gets `agree`.
   - **Refuse**, with an outcome for every held agent: cancel its ticket, give it back to its team's queue, or keep it on hold (the only choice for an agent without a ticket). **Commit first** releases each held agent with a ticket for one short turn, with a notice to commit its work in progress and comment the ticket; the outcome applies at the end of that turn (kept in memory: a daemon restart drops it, and the agent stays held). The director gets `refuse`.

A request whose budget is no longer reached before the human decides is closed, with the reason: the monthly reset, budgets turned off, or the budget raised, removed or its work finished.

## The director's caps

The director reads the budgets (`budget_list`) and proposes agent, team or staff caps (`budget_propose`). A proposal (`BP-<n>`) reaches the human as a `propose` message, shown as a card: accept it, possibly with another limit, or refuse it with a reason. The director gets `agree` or `refuse`; only acceptance writes `budgets.yaml`.

## In the control room

- **The spending board**, an easel on the office wall, opens the spending popup: a month's total, the spend by team, agent, feature and scheduled task, every budget's gauge, and a month selector.
- **Gauges**: under each desk's name plate, the budget closest to its threshold among those covering the agent (the tooltip lists them all); under a team room's sign, the team's cap.
- **Budget hold**: an empty wallet above the desk, a red status in the chat rail and Staff, a flag on the ticket's card.
- **Spent / budget** on ticket cards and drawers and in the feature banner; each occurrence's cost in a schedule's history.
- **Fields**: a budget when starting a feature and on the director's proposal card, when creating a ticket (with what the feature has left) and in its drawer; agent caps in the agent editor, team caps in the team dialog, the staff cap, the threshold and the switch in Settings → Budgets. They hide while budgets are off.
- **Cards** in the director's chat: extension requests (Extend / Refuse with a common outcome, "per agent", "commit first") and cap proposals.

## API

`/api/spend` (a month's spend and every gauge), `/api/budgets` (the settings), `PATCH /api/tickets/{id}` (a ticket's budget), `/api/budget-requests` and `/{id}/extend|refuse`, `/api/budget-proposals` and `/{id}/accept|refuse`; a budget field when starting a feature; events `spend.changed` (at most once a second) and `budgets.changed`. Details in `api/openapi.yaml`.

## Code

| Piece | Role |
|---|---|
| `cmd/nobody-hook` (`statusline` mode) | The statusLine relay. |
| `internal/budget` | The meter (`meter.go`), settings and their file (`settings.go`, `file.go`, `book.go`), gauges (`gauges.go`), coverage and evaluation (`coverage.go`), requests and proposals (`request.go`, `proposal.go`). |
| `internal/daemon/spend.go` | Wiring of the meter: attribution, forks, `spend.changed`. |
| `internal/daemon/budgets.go` | The guard: holds, requests, the refused `Stop`, claims, the monthly reset. |
| `internal/daemon/budgetoffice.go`, `budgetproposals.go` | Requests and decisions, cap proposals, as messages. |
| `internal/work/budget.go` | Ticket and feature budgets, what is left to allocate, extensions. |
| `internal/store/spend.go`, `budgets.go` | Spend records and totals, requests, extensions, proposals (migrations `0008` to `0010`). |
| `internal/officemcp/budgets.go` | `budget_request`; the director's `budget_list`, `budget_submit`, `budget_propose`. |
| `internal/server/budgets.go` | The HTTP endpoints. |
| `web/src/components/budgets/`, `office/SpendPanel.tsx`, `chat/cards.tsx` | Fields, the spending popup, the cards. |
| `test/e2e/budgets_test.go` | Whole scenarios with fakeclaude's statusLine. |
