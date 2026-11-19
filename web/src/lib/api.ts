// Client for the nobodyd API (see api/openapi.yaml at the repository root).
//
// The control room talks to the launcher (ADR-0014): a window shows one
// project, whose daemon answers under /p/<project>/api/... Every call below
// goes to the project set by setProject; the launcher's own routes (projects,
// folders, holding a project) are in launcher.ts.

let projectBase = "";

// setProject points every call of this module at a project's daemon. The
// window calls it once, before rendering the project.
export function setProject(id: string): void {
  projectBase = `/p/${encodeURIComponent(id)}`;
}

// projectPath is path ("/api/...") on the current project's daemon.
export function projectPath(path: string): string {
  return projectBase + path;
}

// daemonFetch is fetch on the current project's daemon.
function daemonFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(projectPath(path), init);
}

// budget_hold: a budget at its threshold holds the agent, its session kept (ADR-0013).
export type AgentStatus = "stopped" | "starting" | "working" | "idle" | "exited" | "awaiting_approval" | "budget_hold";

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
  // Skills preloaded: handed to the agent at every session start (ADR-0011).
  skills?: string[];
  // Skills the agent does not get, built-in ones included.
  skills_exclude?: string[];
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
  const res = await daemonFetch(path, {
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
  const res = await daemonFetch(path, { method });
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
  const res = await daemonFetch(`/api/approvals/${encodeURIComponent(id)}`, {
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
  // Its budget, if any (for a proposed feature, the one the director
  // proposes); what its tickets spent, and what they take out of the budget.
  budget_usd?: number;
  threshold?: number;
  spent_usd: number;
  allocated_usd: number;
}

// startProposedFeature starts a feature the director proposed. A budget
// replaces the one proposed (budget_usd null removes it); without one, the
// request has no body and the proposed budget is kept.
export async function startProposedFeature(id: string, budget?: BudgetFields): Promise<Feature> {
  const path = `/api/features/${encodeURIComponent(id)}/start`;
  if (budget) return postJSON<Feature>(path, budget);

  const res = await daemonFetch(path, { method: "POST", headers: { "Content-Type": "application/json" } });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as Feature;
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

export function startFeature(draft: {
  title: string;
  description: string;
  base_branch: string;
  budget_usd?: number;
  threshold?: number;
}): Promise<Feature> {
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
  budget_usd?: number;
  threshold?: number;
  // What was spent on it, all time (ADR-0013).
  spent_usd: number;
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
  // Required inside a feature with a budget, at most what it has left.
  budget_usd?: number;
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
  const res = await daemonFetch(`/api/tickets/${encodeURIComponent(id)}${cascade ? "?cascade=true" : ""}`, { method: "DELETE" });
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
  const res = await daemonFetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as T;
}

// launcherWS is where WebSockets (terminals, holding a project) reach the
// launcher, which passes a terminal on to the project's daemon:
//   - the page's own origin when the launcher serves the control room (the
//     static export, ADR-0016);
//   - with `next dev`, straight to the launcher, ws://127.0.0.1:7420 by
//     default: the Next.js proxy does not carry WebSockets.
export function launcherWS(): string {
  const configured = process.env.NEXT_PUBLIC_NOBODY_DAEMON_WS;
  if (configured) return configured;
  if (process.env.NEXT_PUBLIC_NOBODY_STATIC === "1") {
    return `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}`;
  }
  return "ws://127.0.0.1:7420";
}

export function terminalURL(address: string): string {
  return `${launcherWS()}${projectPath(`/api/agents/${encodeURIComponent(address)}/terminal`)}`;
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
  const res = await daemonFetch("/api/messages?scope=agents", { method: "DELETE" });
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

// MCPServerInput adds a server to one scope. The server is in Claude Code's
// format; every env and headers value must refer to an environment variable
// (${NAME}): secrets are never written.
export interface MCPServerInput {
  name: string;
  scope: MCPScope;
  server: {
    type: "stdio" | "http" | "sse";
    command?: string;
    args?: string[];
    url?: string;
    env?: Record<string, string>;
    headers?: Record<string, string>;
  };
}

// addMCPServer writes the server through Claude Code; every agent restarts
// once idle to get it. Returns the servers after the change.
export function addMCPServer(input: MCPServerInput): Promise<MCPServer[]> {
  return postJSON<MCPServer[]>("/api/mcp", input);
}

// removeMCPServer removes the server from its scope; every agent restarts
// once idle.
export function removeMCPServer(name: string, scope: MCPScope): Promise<void> {
  return send("DELETE", `/api/mcp/${encodeURIComponent(name)}?scope=${scope}`);
}

// MCPServerDetail is a server to edit: its command, arguments and URL (to
// hide until revealed), and env and headers whose values written in clear
// come back empty, listed in clear_values ("env.NAME", "headers.NAME").
export interface MCPServerDetail {
  name: string;
  source: MCPScope;
  server: MCPServerInput["server"];
  clear_values: string[];
}

export function getMCPServer(name: string): Promise<MCPServerDetail> {
  return getJSON<MCPServerDetail>(`/api/mcp/${encodeURIComponent(name)}`);
}

// updateMCPServer changes a server in its scope, keeping the keys the form
// does not know; every agent restarts once idle.
export function updateMCPServer(name: string, scope: MCPScope, server: MCPServerInput["server"]): Promise<MCPServer[]> {
  return sendJSON<MCPServer[]>("PUT", `/api/mcp/${encodeURIComponent(name)}`, { scope, server });
}

export interface MCPTool {
  name: string;
  description?: string;
}

// listMCPTools has the daemon start the server and ask for its tools.
export function listMCPTools(name: string): Promise<MCPTool[]> {
  return getJSON<MCPTool[]>(`/api/mcp/${encodeURIComponent(name)}/tools`);
}

// --- Skills (ADR-0011): every agent gets every skill, but those it excludes ---

// Where a skill the control room can change lives: the project's
// .claude/skills (committed), or the user's personal skills (all projects).
export type SkillScope = "project" | "personal";

export interface Skill {
  name: string;
  description: string;
  scope: SkillScope | "builtin";
  // Invoking it asks for permission: it goes to the approval queue.
  has_hooks: boolean;
  // Project skills only: committed and unchanged. Agents working on a
  // feature only see committed skills.
  committed?: boolean;
  // Personal skills only: a project skill has the same name and wins.
  shadowed?: boolean;
}

export interface SkillList {
  skills: Skill[];
  // Why the built-in skills could not be discovered.
  builtin_error?: string;
}

// SkillDetail is a skill to edit; other_keys is the rest of its header
// (allowed-tools, hooks...), read-only and kept when saving.
export interface SkillDetail {
  name: string;
  scope: SkillScope;
  description: string;
  body: string;
  other_keys: string;
  has_hooks: boolean;
}

export const SKILL_NAME_PATTERN = "[a-z0-9][a-z0-9\\-]{0,63}";

export function listSkills(): Promise<SkillList> {
  return getJSON<SkillList>("/api/skills");
}

// refreshBuiltinSkills has the daemon discover Claude Code's built-in skills
// again, without calling the model.
export function refreshBuiltinSkills(): Promise<SkillList> {
  return postJSON<SkillList>("/api/skills/builtin/refresh", {});
}

export function getSkill(scope: SkillScope, name: string): Promise<SkillDetail> {
  return getJSON<SkillDetail>(`/api/skills/${scope}/${encodeURIComponent(name)}`);
}

// saveSkill creates or changes a skill's SKILL.md; agents restart once idle.
export function saveSkill(scope: SkillScope, name: string, description: string, body: string): Promise<SkillList> {
  return sendJSON<SkillList>("PUT", `/api/skills/${scope}/${encodeURIComponent(name)}`, { description, body });
}

// deleteSkill removes the skill's whole directory.
export function deleteSkill(scope: SkillScope, name: string): Promise<void> {
  return send("DELETE", `/api/skills/${scope}/${encodeURIComponent(name)}`);
}

// copySkillToProject copies a personal skill into the project, where it then
// shadows the personal one.
export function copySkillToProject(name: string): Promise<SkillList> {
  return postJSON<SkillList>(`/api/skills/personal/${encodeURIComponent(name)}/copy-to-project`, {});
}

// --- Commands (ADR-0017): every agent gets every command ---

// Where a command lives: the project's .claude/commands (committed), or the
// user's personal commands (all projects).
export type CommandScope = "project" | "personal";

export interface Command {
  // How it is invoked, without the "/": front/test.md is front:test.
  name: string;
  // Empty when the header has none.
  description: string;
  argument_hint?: string;
  // Tools the command lets run without asking while it runs, outside the
  // approval queue.
  allowed_tools?: string;
  scope: CommandScope;
  // Project commands only: committed and unchanged. Agents working on a
  // feature only see committed commands.
  committed?: boolean;
  // Personal commands only: a project command has the same name and wins.
  shadowed?: boolean;
}

export interface CommandList {
  commands: Command[];
}

// CommandDetail is a command to edit; other_keys is the rest of its header
// (argument-hint, allowed-tools, model...), read-only and kept when saving.
export interface CommandDetail {
  name: string;
  scope: CommandScope;
  description: string;
  body: string;
  other_keys: string;
  allowed_tools?: string;
}

// The name of a new command: segments of lowercase letters, digits and
// hyphens, ":" for a sub-directory.
export const COMMAND_NAME_PATTERN = "[a-z0-9][a-z0-9\\-]{0,63}(:[a-z0-9][a-z0-9\\-]{0,63})*";

export function listCommands(): Promise<CommandList> {
  return getJSON<CommandList>("/api/commands");
}

export function getCommand(scope: CommandScope, name: string): Promise<CommandDetail> {
  return getJSON<CommandDetail>(`/api/commands/${scope}/${encodeURIComponent(name)}`);
}

// saveCommand creates or changes a command's file; agents restart once idle.
export function saveCommand(scope: CommandScope, name: string, description: string, body: string): Promise<CommandList> {
  return sendJSON<CommandList>("PUT", `/api/commands/${scope}/${encodeURIComponent(name)}`, { description, body });
}

// deleteCommand removes the command's file.
export function deleteCommand(scope: CommandScope, name: string): Promise<void> {
  return send("DELETE", `/api/commands/${scope}/${encodeURIComponent(name)}`);
}

// copyCommandToProject copies a personal command into the project, where it
// then shadows the personal one.
export function copyCommandToProject(name: string): Promise<CommandList> {
  return postJSON<CommandList>(`/api/commands/personal/${encodeURIComponent(name)}/copy-to-project`, {});
}

export async function sendMessage(draft: HumanDraft): Promise<Message> {
  const res = await daemonFetch("/api/messages", {
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
  const res = await daemonFetch(`/api/agents/${encodeURIComponent(address)}/start`, { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await daemonFetch(path, { cache: "no-store" });
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

// --- Scheduled tasks (ADR-0012, docs/schedules.md) ---

export type ScheduleMode = "ticket" | "message";

// A scheduled task, as written in .nobody/schedules.yaml. Exactly one of
// every and cron is set.
export interface Schedule {
  id: string;
  title: string;
  every?: string;
  cron?: string;
  // An agent ("monitor@ops"), a team ("@ops") or the director.
  target: string;
  mode: ScheduleMode;
  // Read-only: false when paused, or disabled because its target is gone.
  enabled?: boolean;
  instructions: string;
}

export type RunStatus = "waiting" | "running" | "done" | "canceled" | "skipped" | "unavailable" | "interrupted";

// One occurrence of a schedule.
export interface ScheduleRun {
  id: number;
  schedule_id: string;
  schedule_title: string;
  trigger: "schedule" | "manual";
  status: RunStatus;
  due_at: string;
  started_at?: string;
  ended_at?: string;
  agent?: string;
  ticket?: string;
  message?: string;
  note?: string;
  // What its agent spent while running it (ADR-0013).
  spent_usd: number;
}

export interface ScheduleView extends Schedule {
  enabled: boolean;
  next_run?: string;
  last_run?: ScheduleRun;
  warning?: string;
}

export interface ScheduleProposal {
  id: string;
  schedule: Schedule;
  status: "proposed" | "accepted" | "refused";
  proposed_by: string;
  proposal_message?: string;
  refusal_reason?: string;
  warning?: string;
  created_at: string;
  decided_at?: string;
}

export const SCHEDULE_ID_PATTERN = "[a-z0-9][a-z0-9\\-]{0,63}";

export function listSchedules(): Promise<ScheduleView[]> {
  return getJSON<ScheduleView[]>("/api/schedules");
}

export function createSchedule(s: Schedule): Promise<ScheduleView> {
  return postJSON<ScheduleView>("/api/schedules", withoutEnabled(s));
}

export function updateSchedule(id: string, s: Schedule): Promise<ScheduleView> {
  return sendJSON<ScheduleView>("PUT", `/api/schedules/${encodeURIComponent(id)}`, withoutEnabled(s));
}

export function deleteSchedule(id: string): Promise<void> {
  return send("DELETE", `/api/schedules/${encodeURIComponent(id)}`);
}

export function setScheduleEnabled(id: string, enabled: boolean): Promise<ScheduleView> {
  return postJSON<ScheduleView>(`/api/schedules/${encodeURIComponent(id)}/${enabled ? "resume" : "pause"}`, {});
}

export function runScheduleNow(id: string): Promise<ScheduleRun> {
  return postJSON<ScheduleRun>(`/api/schedules/${encodeURIComponent(id)}/run`, {});
}

// previewRecurrence returns the next three occurrences of every or cron, or
// throws the daemon's explanation of what is wrong.
export async function previewRecurrence(r: { every?: string; cron?: string }): Promise<string[]> {
  return (await postJSON<{ next: string[] }>("/api/schedules/preview", r)).next;
}

export function listScheduleRuns(schedule = "", limit = 100): Promise<ScheduleRun[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (schedule) params.set("schedule", schedule);
  return getJSON<ScheduleRun[]>(`/api/schedule-runs?${params}`);
}

export function listScheduleProposals(): Promise<ScheduleProposal[]> {
  return getJSON<ScheduleProposal[]>("/api/schedule-proposals");
}

// acceptScheduleProposal writes the proposal, as edited by the human if
// edited is given.
export function acceptScheduleProposal(id: string, edited?: Schedule): Promise<ScheduleProposal> {
  return postJSON<ScheduleProposal>(`/api/schedule-proposals/${encodeURIComponent(id)}/accept`, edited ? { schedule: withoutEnabled(edited) } : {});
}

export function refuseScheduleProposal(id: string, reason: string): Promise<ScheduleProposal> {
  return postJSON<ScheduleProposal>(`/api/schedule-proposals/${encodeURIComponent(id)}/refuse`, { reason });
}

// withoutEnabled drops what the API does not accept in a schedule: enabled
// is changed by pause and resume, and the view's extra fields are read-only.
function withoutEnabled(s: Schedule): Schedule {
  const { id, title, every, cron, target, mode, instructions } = s;
  return { id, title, ...(every ? { every } : {}), ...(cron ? { cron } : {}), target, mode, instructions };
}

// --- Costs and budgets (ADR-0013, docs/budgets.md) ---
// Amounts are Claude Code's estimate at list price, in USD: a guard, not an invoice.

// A ticket's or feature's budget; budget_usd null removes it.
export interface BudgetFields {
  budget_usd: number | null;
  threshold?: number | null;
}

// An agent, team or staff cap.
export interface BudgetCap {
  limit_usd: number;
  // Starts again from 0 every month; otherwise cumulative.
  monthly?: boolean;
  // Overrides the project's threshold.
  threshold?: number;
}

// .nobody/budgets.yaml: whether budgets are on, the project's threshold, and
// the agent, team and staff caps.
export interface BudgetSettings {
  enabled: boolean;
  threshold: number;
  staff?: BudgetCap;
  teams?: Record<string, BudgetCap>;
  agents?: Record<string, BudgetCap>;
}

export type BudgetKind = "ticket" | "feature" | "agent" | "team" | "staff";

export interface BudgetScope {
  kind: BudgetKind;
  // The ticket, feature, agent address or team id; absent for the staff.
  id?: string;
}

// Where one budget stands, over its own period.
export interface BudgetGauge {
  scope: BudgetScope;
  limit_usd: number;
  spent_usd: number;
  threshold: number;
  monthly?: boolean;
  // At its threshold: it holds its agents.
  held?: boolean;
}

export interface SpendLine {
  key: string;
  label?: string;
  spent_usd: number;
}

export interface SpendSummary {
  month: string;
  total_usd: number;
  agents: SpendLine[];
  teams: SpendLine[];
  features: SpendLine[];
  schedules: SpendLine[];
  gauges: BudgetGauge[];
}

export type BudgetOutcome = "cancel" | "release" | "hold";

export interface BudgetRefusal {
  outcomes: Record<string, BudgetOutcome>;
  commit_first?: boolean;
  reason?: string;
}

export type BudgetRequestStatus = "awaiting_agent" | "awaiting_director" | "awaiting_human" | "extended" | "refused" | "closed";

// The one extension request of a budget at its threshold.
export interface BudgetRequest {
  id: string;
  scope: BudgetScope;
  status: BudgetRequestStatus;
  active: boolean;
  held_agents: string[];
  requested_by?: string;
  amount_usd?: number;
  justification?: string;
  director_opinion?: string;
  recommended_usd?: number;
  limit_usd: number;
  spent_usd: number;
  feature_remaining_usd?: number;
  // The director's message to the human that carries the card.
  card_message?: string;
  extended_usd?: number;
  refusal?: BudgetRefusal;
  closed_reason?: string;
  created_at: string;
  submitted_at?: string;
  decided_at?: string;
}

export interface BudgetProposal {
  id: string;
  scope: BudgetScope;
  cap: BudgetCap;
  reason?: string;
  status: "proposed" | "accepted" | "refused";
  proposed_by: string;
  proposal_message?: string;
  refusal_reason?: string;
  created_at: string;
  decided_at?: string;
}

export function getBudgets(): Promise<BudgetSettings> {
  return getJSON<BudgetSettings>("/api/budgets");
}

export function updateBudgets(settings: BudgetSettings): Promise<BudgetSettings> {
  return sendJSON<BudgetSettings>("PUT", "/api/budgets", settings);
}

// getSpend reads a month's spend ("YYYY-MM"), the current one by default.
export function getSpend(month?: string): Promise<SpendSummary> {
  return getJSON<SpendSummary>(month ? `/api/spend?month=${encodeURIComponent(month)}` : "/api/spend");
}

// updateTicketBudget changes a ticket's budget (the human only).
export function updateTicketBudget(id: string, budget: BudgetFields): Promise<Ticket> {
  return sendJSON<Ticket>("PATCH", `/api/tickets/${encodeURIComponent(id)}`, budget);
}

export function listBudgetRequests(): Promise<BudgetRequest[]> {
  return getJSON<BudgetRequest[]>("/api/budget-requests");
}

export function extendBudget(id: string, amountUSD: number): Promise<BudgetRequest> {
  return postJSON<BudgetRequest>(`/api/budget-requests/${encodeURIComponent(id)}/extend`, { amount_usd: amountUSD });
}

export function refuseBudget(id: string, refusal: BudgetRefusal): Promise<BudgetRequest> {
  return postJSON<BudgetRequest>(`/api/budget-requests/${encodeURIComponent(id)}/refuse`, refusal);
}

export function listBudgetProposals(): Promise<BudgetProposal[]> {
  return getJSON<BudgetProposal[]>("/api/budget-proposals");
}

// acceptBudgetProposal writes the proposed cap, as edited by the human if cap
// is given.
export function acceptBudgetProposal(id: string, cap?: BudgetCap): Promise<BudgetProposal> {
  return postJSON<BudgetProposal>(`/api/budget-proposals/${encodeURIComponent(id)}/accept`, cap ? { cap } : {});
}

export function refuseBudgetProposal(id: string, reason: string): Promise<BudgetProposal> {
  return postJSON<BudgetProposal>(`/api/budget-proposals/${encodeURIComponent(id)}/refuse`, { reason });
}

// usd formats an amount: "$0.42", "$12.50".
export function usd(amount: number): string {
  return `$${amount.toFixed(2)}`;
}

// scopeLabel names a budget for people: "ticket T-12", "team dev", "the staff".
export function scopeLabel(scope: BudgetScope): string {
  return scope.kind === "staff" ? "the staff" : `${scope.kind} ${scope.id ?? ""}`;
}
