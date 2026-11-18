// The daemon's event stream, GET /api/events (docs/events.md).
//
// Most events only say what changed: the UI reads it again through the REST
// API. agent.status and message.sent carry data, used by the office
// animations. "hello" opens every connection: reload everything.

import type { AgentStatus, Message } from "./api";

export type DaemonEvent =
  | { type: "hello" }
  | { type: "agent.status"; data: { address: string; status: AgentStatus } }
  | { type: "message.sent"; data: Message }
  | { type: "message.delivered"; data: { recipient: string; ids: string[] } }
  | { type: "messages.deleted"; data: { ids: string[] } }
  | { type: "approvals.changed" }
  | { type: "tickets.changed"; data: { id: string } }
  | { type: "features.changed"; data: { id: string } }
  | { type: "org.changed" }
  | { type: "catalog.changed" }
  | { type: "schedules.changed" };

export type EventType = DaemonEvent["type"];

const EVENT_TYPES: EventType[] = [
  "hello",
  "agent.status",
  "message.sent",
  "message.delivered",
  "messages.deleted",
  "approvals.changed",
  "tickets.changed",
  "features.changed",
  "org.changed",
  "catalog.changed",
  "schedules.changed",
];

// connectEvents opens the stream and calls onEvent for each event, and
// onConnection when the connection opens or drops. EventSource reconnects by
// itself; the daemon then sends "hello" again. It returns a function closing
// the stream.
export function connectEvents(onEvent: (e: DaemonEvent) => void, onConnection: (connected: boolean) => void): () => void {
  const source = new EventSource("/api/events");

  source.onopen = () => onConnection(true);
  source.onerror = () => onConnection(false);

  for (const type of EVENT_TYPES) {
    source.addEventListener(type, (raw) => {
      const data: unknown = raw.data ? JSON.parse(raw.data) : {};
      onEvent({ type, data } as DaemonEvent);
    });
  }

  return () => source.close();
}
