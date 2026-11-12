// Client for the nobodyd API (see api/openapi.yaml at the repository root).

export type AgentStatus = "stopped" | "starting" | "working" | "idle" | "exited" | "awaiting_approval";

export interface Agent {
  address: string;
  team: string;
  name: string;
  role: string;
  status: AgentStatus;
}

export const ACTS = ["request", "query", "propose", "inform", "agree", "refuse", "done"] as const;
export type Act = (typeof ACTS)[number];
export type Priority = "normal" | "urgent";

export interface Message {
  id: string;
  conversation: string;
  in_reply_to?: string;
  from: string;
  to: string;
  act: Act;
  priority: Priority;
  subject: string;
  body: string;
  hops: number;
  ticket?: string;
  sent_at: string;
  delivered_at?: string;
}

export interface HumanDraft {
  to: string;
  act: Act;
  priority: Priority;
  subject: string;
  body: string;
  in_reply_to?: string;
}

export const HUMAN = "human";

// An agent's permission request waiting for the human (see docs/approvals.md).
export interface Approval {
  id: string;
  agent: string;
  tool_name: string;
  tool_input?: Record<string, unknown>;
  created_at: string;
}

export function listApprovals(): Promise<Approval[]> {
  return getJSON<Approval[]>("/api/approvals");
}

export async function decideApproval(id: string, allow: boolean, message = ""): Promise<void> {
  const res = await fetch(`/api/approvals/${encodeURIComponent(id)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision: allow ? "allow" : "deny", message }),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
}

export type TicketStatus = "open" | "in_progress" | "done" | "canceled";

export interface Ticket {
  id: string;
  title: string;
  description?: string;
  acceptance_criteria?: string;
  team: string;
  assignee?: string;
  status: TicketStatus;
  blocked_by?: string[];
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface Deliverable {
  id: number;
  ticket: string;
  author: string;
  kind: "file" | "commit" | "report" | "note";
  reference?: string;
  summary: string;
  created_at: string;
}

export interface TicketComment {
  id: number;
  ticket: string;
  author: string;
  body: string;
  created_at: string;
}

export interface TicketDetails {
  ticket: Ticket;
  deliverables: Deliverable[];
  comments: TicketComment[];
  messages: Message[];
}

export interface TicketDraft {
  title: string;
  team: string;
  description: string;
  acceptance_criteria: string;
  blocked_by: string[];
}

export function listTickets(): Promise<Ticket[]> {
  return getJSON<Ticket[]>("/api/tickets");
}

export function getTicket(id: string): Promise<TicketDetails> {
  return getJSON<TicketDetails>(`/api/tickets/${encodeURIComponent(id)}`);
}

export function createTicket(draft: TicketDraft): Promise<Ticket> {
  return postJSON<Ticket>("/api/tickets", draft);
}

export function commentTicket(id: string, body: string): Promise<TicketComment> {
  return postJSON<TicketComment>(`/api/tickets/${encodeURIComponent(id)}/comments`, { body });
}

export function setTicketStatus(id: string, status: TicketStatus): Promise<Ticket> {
  return postJSON<Ticket>(`/api/tickets/${encodeURIComponent(id)}/status`, { status });
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as T;
}

// Terminals connect straight to the daemon over WebSocket.
const daemonWS = process.env.NEXT_PUBLIC_NOBODY_DAEMON_WS ?? "ws://127.0.0.1:7420";

export function terminalURL(address: string): string {
  return `${daemonWS}/api/agents/${encodeURIComponent(address)}/terminal`;
}

export function listAgents(): Promise<Agent[]> {
  return getJSON<Agent[]>("/api/agents");
}

export function listMessages(limit = 200): Promise<Message[]> {
  return getJSON<Message[]>(`/api/messages?limit=${limit}`);
}

export async function sendMessage(draft: HumanDraft): Promise<Message> {
  const res = await fetch("/api/messages", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(draft),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as Message;
}

export async function startAgent(address: string): Promise<void> {
  const res = await fetch(`/api/agents/${encodeURIComponent(address)}/start`, { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as T;
}

// errorText extracts the daemon's {"error": "..."} message when there is one.
async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}
