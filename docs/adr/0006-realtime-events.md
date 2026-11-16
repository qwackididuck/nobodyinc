# ADR-0006 — Changes pushed to the control room with Server-Sent Events

- Status: accepted
- Date: 2026-11-16

## Context

The control room polled the daemon: agents and messages every 2 seconds, approvals every second, tickets, features and templates every 2 to 5 seconds. It wastes requests while nothing happens, shows changes up to a few seconds late, and cannot drive animations: the redesigned UI shows a paper plane flying from one agent's desk to another when a message is sent, and agents walking in when they are recruited. It needs to be told when things happen.

Terminals already have their own WebSocket (a byte stream in both directions). The rest only flows from the daemon to the browser.

## Decision

### 1. One Server-Sent Events stream: `GET /api/events`

A single `text/event-stream` carries every change. SSE fits a one-way flow, works with the browser's `EventSource` (which reconnects by itself), goes through ordinary HTTP proxies and needs no new dependency on either side. A WebSocket would add a protocol for no benefit here.

### 2. Invalidation, not state

Most events only name what changed: `tickets.changed {id}`, `approvals.changed`, `org.changed`... The UI then reads the resource again through the REST API, which stays the **single source of truth**: there is no second representation of tickets or agents to keep in step, and the REST handlers keep their rules and their tests.

Two events carry data because the office animates them as they happen: `agent.status` (address and status) and `message.sent` (the message, as returned by `GET /api/messages`).

### 3. No replay

The daemon keeps no history of events. Every connection starts with `hello`, after which the UI reloads everything; a client that falls too far behind is dropped and reconnects the same way. This keeps the daemon simple and the UI always correct after a reconnect, at the cost of one full reload, which is what polling did every few seconds anyway.

### 4. Where events come from

The services keep reporting through their own hooks (`Statuses.OnChange`, `Store.OnDelivered`, `messaging.Service.OnSent`, `approval.Queue.OnChange`, `staffing.Service.OnReload`); the daemon wires them to an in-memory bus (`internal/events`). The ticket service's store is wrapped by the daemon so that every ticket and feature write is announced, without the ticket service knowing about the control room.

### 5. Connection details

- A comment line is sent every 15 seconds: the Next.js development proxy closes a connection that stays silent for 30 seconds.
- In development the UI reaches the stream through the Next.js `/api` proxy, on its own origin (no CORS). How the UI is served in production (static export served by the daemon, or a desktop shell) is left open; the stream works the same way from the daemon's origin.
- The stream ends when the daemon stops (the bus is closed with the daemon's context), since an HTTP server's `Shutdown` does not interrupt streaming responses.
- `GET` requests pass the Origin/JSON guard (it protects changes only); the stream reveals nothing more than the REST API it points to.

## Consequences

- The UI no longer polls; it reloads only what changed.
- A new kind of change needs a new event type: documented in `api/openapi.yaml` and `docs/events.md`, published where the change happens.
- The office animations depend on `message.sent` and `agent.status` arriving in order; a dropped client misses animations (not state) until it reconnects.
