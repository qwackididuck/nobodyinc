// Client for the nobodyd API (see api/openapi.yaml at the repository root).

export type AgentStatus = "stopped" | "starting" | "working" | "idle" | "exited" | "awaiting_approval";

export interface Agent {
  address: string;
  team: string;
  name: string;
  role: string;
  // What to ask this agent about, and what it decides.
  expertise?: string;
  status: AgentStatus;
  on_leave: boolean;
  // Template it was recruited from, "<template>@<version>" (information only).
  from?: string;
  // A change waits for the agent to end its turn before it restarts or stops.
  pending_change: boolean;
}

// --- Teams, agent definitions, templates and recruitment (see docs/agents.md) ---

export interface Team {
  id: string;
  goal?: string;
  // Instructions shared by the team's agents (.nobody/teams/<id>/TEAM.md);
  // empty means the goal alone.
  prompt?: string;
  // Teams its members may not start a conversation with (ADR-0009): flows
  // are open by default. Answers and tickets are never blocked.
  block?: string[];
}

export const PROFILES = ["default", "allowlist", "auto"] as const;
export type PermissionProfile = (typeof PROFILES)[number];

// Content of an agent or template file. description and version are used by
// templates; from and on_leave by agents (read-only through the API).
export interface AgentDefinition {
  description?: string;
  version?: number;
  role?: string;
  // Public: what colleagues should ask this agent about, and what it decides.
  expertise?: string;
  model?: string;
  permissions?: { profile?: PermissionProfile; allow?: string[]; deny?: string[] };
  tools?: string[];
  // MCP servers ("gitlab") or single tools ("gitlab/delete_project") the
  // agent does not get; it gets every other server (ADR-0010).
  mcp_exclude?: string[];
  skills?: string[];
  from?: string;
  on_leave?: boolean;
  prompt?: string;
}

export interface Template extends AgentDefinition {
  name: string;
  // Shipped with Nobody Inc and not overridden in ~/.nobody/catalog.
  builtin: boolean;
  // A user template replacing a built-in one: deleting it brings it back.
  overrides_builtin?: boolean;
}

// How a change reaches a running agent: fresh starts a new session instead of
// resuming; now does not wait for the end of its current turn.
export interface ApplyOptions {
  fresh?: boolean;
  now?: boolean;
}

export function listTeams(): Promise<Team[]> {
  return getJSON<Team[]>("/api/teams");
}

// createTeam adds a team (a room of the office), without agents yet.
export function createTeam(team: Team): Promise<Team> {
  return postJSON<Team>("/api/teams", team);
}

// updateTeam changes a team's goal, prompt and block list. Its agents restart
// with a fresh session once idle when the goal or prompt changes; a new block
// list only sends them a notice. Without block, the block list stays.
export function updateTeam(id: string, change: { goal: string; prompt: string; block?: string[] }): Promise<Team> {
  return sendJSON<Team>("PUT", `/api/teams/${encodeURIComponent(id)}`, change);
}

// deleteTeam removes a team and dismisses its members: each stops once idle
// (or at once with now). Its tickets stay; those in progress go back to open.
export function deleteTeam(id: string, opts: ApplyOptions = {}): Promise<void> {
  return send("DELETE", `/api/teams/${encodeURIComponent(id)}${query(opts)}`);
}

// The project's shared prompt (.nobody/project.md), given to every agent.
export interface Project {
  prompt: string;
}

export function getProject(): Promise<Project> {
  return getJSON<Project>("/api/project");
}

// setProjectPrompt changes the project prompt; every agent restarts with a
// fresh session once idle.
export function setProjectPrompt(prompt: string): Promise<Project> {
  return sendJSON<Project>("PUT", "/api/project", { prompt });
}

export function listTemplates(): Promise<Template[]> {
  return getJSON<Template[]>("/api/catalog");
}

export function saveTemplate(name: string, definition: AgentDefinition): Promise<Template> {
  return sendJSON<Template>("PUT", `/api/catalog/${encodeURIComponent(name)}`, definition);
}

// deleteTemplate removes a user template (built-in ones cannot be deleted).
export function deleteTemplate(name: string): Promise<void> {
  return send("DELETE", `/api/catalog/${encodeURIComponent(name)}`);
}

// recruitAgent hires from a template, or creates from a definition.
export function recruitAgent(
  team: string,
  body: { name: string; template: string } | { name: string; definition: AgentDefinition },
): Promise<Agent> {
  return postJSON<Agent>(`/api/teams/${encodeURIComponent(team)}/agents`, body);
}

export function getDefinition(address: string): Promise<AgentDefinition> {
  return getJSON<AgentDefinition>(`/api/agents/${encodeURIComponent(address)}/definition`);
}

export function editDefinition(address: string, definition: AgentDefinition, opts: ApplyOptions): Promise<AgentDefinition> {
  return sendJSON<AgentDefinition>("PUT", `/api/agents/${encodeURIComponent(address)}/definition${query(opts)}`, definition);
}

export function putOnLeave(address: string, opts: ApplyOptions = {}): Promise<void> {
  return send("POST", `/api/agents/${encodeURIComponent(address)}/leave${query(opts)}`);
}

export function returnFromLeave(address: string, opts: ApplyOptions = {}): Promise<void> {
  return send("POST", `/api/agents/${encodeURIComponent(address)}/return${query(opts)}`);
}

export function dismissAgent(address: string, opts: ApplyOptions = {}): Promise<void> {
  return send("DELETE", `/api/agents/${encodeURIComponent(address)}${query(opts)}`);
}

function query(opts: ApplyOptions): string {
  const params = new URLSearchParams();
  if (opts.fresh) params.set("fresh", "true");
  if (opts.now) params.set("now", "true");
  const q = params.toString();
  return q ? `?${q}` : "";
}

async function sendJSON<T>(method: string, path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as T;
}

// send makes a request whose answer has no body (202, 204).
async function send(method: string, path: string): Promise<void> {
  const res = await fetch(path, { method });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
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

// A request the organization works on, with its own git branch (see docs/workspaces.md).
export type FeatureStatus = "proposed" | "started" | "refused" | "finished";

export interface Feature {
  id: string;
  title: string;
  // The need: what is wanted and why.
  description?: string;
  acceptance_criteria?: string;
  slug: string;
  base_branch: string;
  branch: string;
  active: boolean;
  // The director proposes, the human starts or refuses (docs/god.md).
  status: FeatureStatus;
  // The "propose" message the director's proposal sent to the human.
  proposal_message?: string;
  refusal_reason?: string;
  // The merge commit on the base branch, once finished (ADR-0007).
  merge_commit?: string;
  created_by: string;
  created_at: string;
}

export function startProposedFeature(id: string): Promise<Feature> {
  return postJSON<Feature>(`/api/features/${encodeURIComponent(id)}/start`, {});
}

export function refuseProposedFeature(id: string, reason: string): Promise<Feature> {
  return postJSON<Feature>(`/api/features/${encodeURIComponent(id)}/refuse`, { reason });
}

// What a started feature brings to its base branch (GET /api/features/{id}/review).
export interface FeatureReview {
  feature: Feature;
  commits: { hash: string; author: string; date: string; subject: string }[];
  // status: A added, M modified, D deleted, R renamed; added is -1 for a binary file.
  files: { path: string; status: string; added: number; deleted: number }[];
  diff: string;
  diff_truncated: boolean;
  unfinished_tickets: Ticket[];
}

export function reviewFeature(id: string): Promise<FeatureReview> {
  return getJSON<FeatureReview>(`/api/features/${encodeURIComponent(id)}/review`);
}

// finishFeature merges the active feature into its base branch, locally, and
// ends it; force finishes it although some tickets are not done.
export function finishFeature(id: string, force: boolean): Promise<Feature> {
  return postJSON<Feature>(`/api/features/${encodeURIComponent(id)}/finish`, { force });
}

export function listFeatures(): Promise<Feature[]> {
  return getJSON<Feature[]>("/api/features");
}

export function startFeature(draft: { title: string; description: string; base_branch: string }): Promise<Feature> {
  return postJSON<Feature>("/api/features", draft);
}

export type TicketStatus = "open" | "in_progress" | "done" | "canceled";

export interface Ticket {
  id: string;
  title: string;
  description?: string;
  acceptance_criteria?: string;
  team: string;
  feature?: string;
  assignee?: string;
  status: TicketStatus;
  blocked_by?: string[];
  created_by: string;
  created_at: string;
  updated_at: string;
  // The ticket of a finished feature, kept in the archives (read-only).
  archived?: boolean;
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

// listArchivedTickets returns the tickets of finished features.
export function listArchivedTickets(): Promise<Ticket[]> {
  return getJSON<Ticket[]>("/api/tickets?archived=true");
}

// deleteTicket shreds a ticket for good; with cascade, the tickets that
// depend on it (and theirs) go too, otherwise they stop waiting for it.
// It returns the IDs of the deleted tickets.
export async function deleteTicket(id: string, cascade: boolean): Promise<string[]> {
  const res = await fetch(`/api/tickets/${encodeURIComponent(id)}${cascade ? "?cascade=true" : ""}`, { method: "DELETE" });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return ((await res.json()) as { ids: string[] }).ids;
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

// listConversation returns the human's conversation with one address, both
// directions, oldest first: the chat of that agent.
export function listConversation(address: string, limit = 200): Promise<Message[]> {
  return getJSON<Message[]>(`/api/messages?with=${encodeURIComponent(address)}&limit=${limit}`);
}

// listAgentExchanges returns the delivered messages between agents (the
// director included), oldest first: the wastebasket of the control room.
export function listAgentExchanges(limit = 200): Promise<Message[]> {
  return getJSON<Message[]>(`/api/messages?scope=agents&limit=${limit}`);
}

// emptyWastebasket deletes for good the finished conversations between
// agents (every message delivered, the last one expecting no answer); open
// ones stay. It returns the IDs of the deleted messages.
export async function emptyWastebasket(): Promise<string[]> {
  const res = await fetch("/api/messages?scope=agents", { method: "DELETE" });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return ((await res.json()) as { ids: string[] }).ids;
}

// --- Settings (GET/PUT /api/settings) and MCP servers (GET /api/mcp) ---

export type ConfigProtection = "block" | "warn";

export interface Settings {
  project_dir: string;
  project_id: string;
  state_dir: string;
  address: string;
  origins: string[];
  claude_path: string;
  start_agents: boolean;
  config_protection: ConfigProtection;
}

export function getSettings(): Promise<Settings> {
  return getJSON<Settings>("/api/settings");
}

export function setConfigProtection(value: ConfigProtection): Promise<Settings> {
  return sendJSON<Settings>("PUT", "/api/settings", { config_protection: value });
}

// Claude Code's MCP scopes (ADR-0010): project is .mcp.json, committed; local
// is the user on this project only; user is the user on all their projects.
export type MCPScope = "project" | "local" | "user";

// An MCP server every agent gets, without any value in clear.
export interface MCPServer {
  name: string;
  source: MCPScope;
  type: string;
  // The command without its arguments, or the URL without its query.
  target: string;
  // Environment variables the server refers to as ${NAME}; never their value.
  env: { name: string; set: boolean }[];
  // Agents that exclude the whole server.
  excluded_by: string[];
}

export function listMCPServers(): Promise<MCPServer[]> {
  return getJSON<MCPServer[]>("/api/mcp");
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
