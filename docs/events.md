# Events: how the control room hears about changes

The daemon pushes changes to the control room over **Server-Sent Events** on `GET /api/events`. Most events only say *what* changed; the UI then reads it again through the REST API, which stays the source of truth. Decision and rationale: ADR-0006.

## The stream

```
$ curl -N http://127.0.0.1:7420/api/events
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
- A client that falls behind (its buffer of 256 events is full) is dropped; it reconnects and gets `hello` again.

## Event types

| Event | Data | Published when | Source |
|---|---|---|---|
| `hello` | `{}` | a connection opens | `server/events.go` |
| `agent.status` | `{address, status}` | an agent's status actually changes (a repeat is not sent) | `runtime.Statuses.OnChange` |
| `message.sent` | the message | any message is sent (human, agents, director, `nobody`) | `messaging.Service.OnSent` |
| `message.delivered` | `{recipient, ids}` | messages are handed to their recipient (hook, fork, hand-over) | `store.Store.OnDelivered` |
| `approvals.changed` | `{}` | a permission request is added or answered | `approval.Queue.OnChange` |
| `tickets.changed` | `{id}` | a ticket is created or changed, commented, or gets a deliverable | the ticket service's store, wrapped in `daemon/events.go` |
| `features.changed` | `{id}` | a feature is proposed, started, refused or activated | the same wrapper |
| `org.changed` | `{}` | an agent is recruited, created, edited, put on leave, brought back or dismissed (director included) | `staffing.Service.OnReload` |
| `catalog.changed` | `{}` | a template is saved or deleted | `server/staff.go` |

The wiring is in `internal/daemon/daemon.go` (`announce` and `New`); the bus is `internal/events`.

## Adding an event

1. Add the type to `internal/events` (and its data type if it carries any).
2. Publish it where the change happens, through the service's own hook rather than from the HTTP handlers when possible, so that changes made by agents (MCP tools) are announced too.
3. Document it in `api/openapi.yaml` (`/api/events`) and in the table above.
4. Cover it in a test; cross-cutting behaviour gets an end-to-end scenario (`test/e2e/events_test.go`).
