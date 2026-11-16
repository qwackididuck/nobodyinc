// Helpers about agents and messages shared by the chat panel and the office.

import { type Agent, type AgentStatus, HUMAN, type Message, type Team } from "@/lib/api";

// The system address that signs notices written by Nobody Inc itself.
export const SYSTEM = "nobody";

// The director has no team (docs/god.md).
export function isDirector(a: Agent): boolean {
  return a.team === "";
}

// sortAgents puts the director first, then the agents team by team (in the
// order of .nobody/org.yaml), by name within a team.
export function sortAgents(agents: Agent[], teams: Team[]): Agent[] {
  const order = new Map(teams.map((t, i) => [t.id, i]));
  const rank = (a: Agent) => (isDirector(a) ? -1 : (order.get(a.team) ?? teams.length));
  return [...agents].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

export function teamLabel(a: Agent): string {
  return isDirector(a) ? "Leads every team" : a.team;
}

export function statusLabel(a: Agent): string {
  if (a.on_leave) return "On leave";
  const labels: Record<AgentStatus, string> = {
    stopped: "Stopped",
    starting: "Starting",
    working: "Working",
    idle: "Idle",
    exited: "Exited",
    awaiting_approval: "Needs approval",
  };
  return labels[a.status];
}

// statusKey is the status used for colors: leave wins over the process state.
export function statusKey(a: Agent): AgentStatus | "on_leave" {
  return a.on_leave ? "on_leave" : a.status;
}

// withHuman tells whether a message belongs to the human's chat with address.
export function withHuman(m: Message, address: string): boolean {
  return (m.from === HUMAN && m.to === address) || (m.from === address && m.to === HUMAN);
}

// betweenAgents tells whether a message goes from one agent to another
// (the director included): what the wastebasket shows once delivered.
export function betweenAgents(m: Message): boolean {
  const outside = (addr: string) => addr === HUMAN || addr === SYSTEM;
  return !outside(m.from) && !outside(m.to);
}

// isAnswered tells whether the human answered a message.
export function isAnswered(m: Message, messages: Message[]): boolean {
  return messages.some((r) => r.in_reply_to === m.id && r.from === HUMAN);
}

export function timeOf(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
