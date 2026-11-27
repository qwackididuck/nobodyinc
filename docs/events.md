# Events: how the control room hears about changes

A project's daemon pushes changes to the control room over **Server-Sent Events** on `GET /api/events`, which the control room reaches through the launcher as `/p/<project>/api/events` (`projects.md`). Most events only say *what* changed; the UI then reads it again through the REST API, which stays the source of truth. Decision and rationale: ADR-0006.

## The stream

```
$ curl -N http://127.0.0.1:7420/p/shop-6e403867/api/events   # a project open in a window
retry: 2000

event: hello
data: {}

event: message.sent
data: {"id":"m-4f2…","from":"human","to":"archi@specs","act":"request",…}

event: agent.status
data: {"address":"archi@specs","status":"working"}

event: message.delivered
data: {"recipient":"archi@specs","ids":["m-4f2…"]}

: ping
```

- `hello` opens every connection: reload everything. Nothing is replayed.
- `: ping` is sent every 15 seconds so that idle proxies keep the connection open (the Next.js development proxy closes it after 30 seconds of silence).
- `retry` tells `EventSource` to reconnect after 2 seconds if the connection drops.
- `Cache-Control: no-cache, no-transform`: `no-transform` keeps proxies from compressing the stream. The Next.js development server gzips responses for browsers and would otherwise hold every event back.
- A client that falls behind (its buffer of 256 events is full) is dropped; it reconnects and gets `hello` again.

## Event types

| Event | Data | Published when | Source |
|---|---|---|---|
| `hello` | `{}` | a connection opens | `server/events.go` |
| `agent.status` | `{address, status}` | an agent's status actually changes (a repeat is not sent) | `runtime.Statuses.OnChange` |
| `message.sent` | the message | any message is sent (human, agents, director, `nobody`) | `messaging.Service.OnSent` |
| `message.delivered` | `{recipient, ids}` | messages are handed to their recipient (hook, fork, hand-over) | `store.Store.OnDelivered` |
| `messages.deleted` | `{ids}` | messages deleted: the wastebasket was emptied (`DELETE /api/messages?scope=agents`), or open conversations were closed by force (see "Closing open conversations" in `architecture.md`) | `server/api.go`, `messaging/close.go` |
| `approvals.changed` | `{}` | a permission request is added or answered | `approval.Queue.OnChange` |
| `automation.changed` | `{}` | the automatic mode was switched on or off | `automation.Mode.OnChange` |
| `decisions.changed` | `{}` | the director reported a decision taken in the human's place, or the human acknowledged some | `daemon/nobody.go`, `server/automation.go` |
| `tickets.changed` | `{id}` | a ticket is created or changed, commented, or gets a deliverable | the ticket service's store, wrapped in `daemon/events.go` |
| `features.changed` | `{id}` | a feature is proposed, started, refused or activated | the same wrapper |
| `org.changed` | `{}` | the organization is reloaded after a change: an agent recruited, created, edited, put on leave, brought back or dismissed (director included); a team created, edited (goal, prompt, block list) or deleted; the project prompt changed; an MCP server added, edited or removed through the API; the project's subagents imported or dismissed (the control room then reloads `GET /api/subagents` too) | `staffing.Service.OnReload` |
| `catalog.changed` | `{}` | a template is saved or deleted | `server/staff.go` |
| `schedules.changed` | `{}` | a scheduled task is created, edited, paused, resumed, disabled or deleted; one of its occurrences starts, waits, ends or is skipped; a director's proposal is made or decided (`schedules.md`) | `daemon/schedules.go`, `daemon/scheduleoffice.go` |
| `spend.changed` | `{}` | agents spent: a statusLine report or a fork's cost became a spend record; sent at most once a second (`budgets.md`) | `daemon/spend.go` |
| `budgets.changed` | `{}` | `.nobody/budgets.yaml` is written; a budget extension request is opened, justified, passed on, decided or closed; a cap proposal is made or decided (`budgets.md`) | `daemon/budgets.go`, `daemon/budgetoffice.go` |
| `kb.changed` | `{pages, added, removed, changed, reset}` | a scan of the knowledge base found pages added, removed or changed (by an agent or by hand; scans run every few seconds and when it is read), or the knowledge base itself was set up, moved or removed (`reset`: reload without announcing new pages; `knowledge.md`) | `daemon/knowledge.go` |
| `kb.read` | `{agent, tool, subject}` | an agent used a knowledge tool (`kb_search`, `kb_read`, `kb_list`), or a file tool's `PreToolUse` names a file of the knowledge folder (`tool`: `Read`, `Edit`…; `subject`: the file's path in the folder): the office walks it to the library (`knowledge.md`) | `daemon/knowledge.go`, from `officemcp` and the hooks |

The wiring is in `internal/daemon/daemon.go` (`announce` and `New`); the bus is `internal/events`.

## In the office: paper planes and speech bubbles

What the office shows of the messages between agents (`web/src/components/office/`):

- A message between two agents of **different teams**, or with the director, flies as a **paper plane** from desk to desk (`message.sent`), labelled with who writes to whom and the subject unless Settings turns the labels off.
- A message between two **teammates** (same team) is a **speech bubble** above its sender instead, in place of the sender's status bubble, with the act, the recipient, the subject and the start of the body (the whole message when hovering it). Its recipient hops a moment later. The rules (`speech.ts`):
  - a message that expects an answer (`request`, `query`, `propose`) keeps its bubble, with an orange border, until it is answered, however long that takes: a conversation that is stuck shows;
  - one that expects none (`inform`, `agree`, `refuse`, `done`) lasts 2 seconds;
  - an answered message lasts 2 seconds more after its first answer (the answer has a green border), so that, when the answer expects none, question and answer go away together;
  - new bubbles come under the older ones; an agent shows its latest three (two lines of text each), the oldest saying how many more there are;
  - a message to or from an agent that left the organization has no bubble; a deleted message (the wastebasket, `messages.deleted`) loses its.
- The bubbles are computed from the recent messages the control room holds, not from events: a page reloaded shows the same ones. They are a state (who waits for whom), so they also show with animations set to "off", only without the pop-in.

## Adding an event

1. Add the type to `internal/events` (and its data type if it carries any).
2. Publish it where the change happens, through the service's own hook rather than from the HTTP handlers when possible, so that changes made by agents (MCP tools) are announced too.
3. Document it in `api/openapi.yaml` (`/api/events`) and in the table above.
4. Cover it in a test; cross-cutting behaviour gets an end-to-end scenario (`test/e2e/events_test.go`).
