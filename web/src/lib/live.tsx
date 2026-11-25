"use client";

// The control room's view of the daemon, kept up to date by the event stream
// (docs/events.md) instead of polling.
//
// LiveProvider loads every resource through the REST API when the stream
// says "hello", then reloads only what an event says has changed. Reloads are
// grouped: a burst of events for the same resource causes one request. Two
// events are applied directly, without a request: an agent's new status, and
// a sent message (appended to the recent messages). Components that animate
// what happens (the office) receive the raw events through useDaemonEvents.

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

import {
  type Agent,
  type Approval,
  type Automation,
  type Decision,
  type BudgetProposal,
  type BudgetRequest,
  type BudgetSettings,
  type Feature,
  type KnowledgeOverview,
  type Message,
  type ScheduleProposal,
  type ScheduleRun,
  type ScheduleView,
  type Settings,
  type SpendSummary,
  type Subagent,
  type Team,
  type Template,
  type Ticket,
  getAutomation,
  getBudgets,
  getKnowledge,
  getSettings,
  getSpend,
  listAgents,
  listApprovals,
  listBudgetProposals,
  listDecisions,
  listBudgetRequests,
  listFeatures,
  listMessages,
  listScheduleProposals,
  listScheduleRuns,
  listSchedules,
  listSubagents,
  listTeams,
  listTemplates,
  listTickets,
} from "./api";
import { connectEvents, type DaemonEvent } from "./events";

// How many recent messages of the whole organisation are kept (badges, office).
const RECENT_MESSAGES = 500;
// How many recent occurrences of scheduled tasks are kept (office clock).
const RECENT_RUNS = 100;
// Events arriving within this delay cause a single reload.
const RELOAD_DELAY_MS = 120;

interface Resources {
  agents: Agent[];
  teams: Team[];
  approvals: Approval[];
  // The automatic mode and the decisions the director took in the human's
  // place, newest first (docs/automation.md).
  automation: Automation | null;
  decisions: Decision[];
  features: Feature[];
  tickets: Ticket[];
  templates: Template[];
  messages: Message[];
  settings: Settings | null;
  schedules: ScheduleView[];
  scheduleRuns: ScheduleRun[]; // newest first
  scheduleProposals: ScheduleProposal[];
  // Costs and budgets (ADR-0013): the settings, this month's spend with every
  // gauge, the extension requests and the director's cap proposals.
  budgets: BudgetSettings | null;
  spend: SpendSummary | null;
  budgetRequests: BudgetRequest[]; // newest first
  budgetProposals: BudgetProposal[];
  // The project's Claude Code subagents and where each stands (ADR-0018).
  subagents: Subagent[];
  // The knowledge base's settings and overview (ADR-0019).
  knowledge: KnowledgeOverview | null;
}

export type Resource = keyof Resources;

const EMPTY: Resources = {
  agents: [],
  teams: [],
  approvals: [],
  automation: null,
  decisions: [],
  features: [],
  tickets: [],
  templates: [],
  messages: [],
  settings: null,
  schedules: [],
  scheduleRuns: [],
  scheduleProposals: [],
  budgets: null,
  spend: null,
  budgetRequests: [],
  budgetProposals: [],
  subagents: [],
  knowledge: null,
};

const LOADERS: { [K in Resource]: () => Promise<Resources[K]> } = {
  agents: listAgents,
  teams: listTeams,
  approvals: listApprovals,
  automation: getAutomation,
  decisions: listDecisions,
  features: listFeatures,
  tickets: listTickets,
  templates: listTemplates,
  messages: () => listMessages(RECENT_MESSAGES),
  settings: getSettings,
  schedules: listSchedules,
  scheduleRuns: () => listScheduleRuns("", RECENT_RUNS),
  scheduleProposals: listScheduleProposals,
  budgets: getBudgets,
  spend: () => getSpend(),
  budgetRequests: listBudgetRequests,
  budgetProposals: listBudgetProposals,
  subagents: listSubagents,
  knowledge: getKnowledge,
};

// What each invalidation event makes stale.
const STALE: Partial<Record<DaemonEvent["type"], Resource[]>> = {
  "approvals.changed": ["approvals"],
  "automation.changed": ["automation", "approvals"],
  "decisions.changed": ["decisions"],
  "tickets.changed": ["tickets"],
  "features.changed": ["features"],
  "catalog.changed": ["templates"],
  // A schedule's warning depends on its target's profile; a subagent's
  // status on the agents and org.yaml, and so does the knowledge base.
  "org.changed": ["agents", "teams", "schedules", "subagents", "knowledge"],
  "kb.changed": ["knowledge"],
  "schedules.changed": ["schedules", "scheduleRuns", "scheduleProposals"],
  // Tickets, features and occurrences carry what they spent.
  "spend.changed": ["spend", "tickets", "features", "scheduleRuns"],
  // Budgets change the gauges, the tickets' and features' budgets, and the
  // requests that hold agents.
  "budgets.changed": ["budgets", "spend", "budgetRequests", "budgetProposals", "tickets", "features"],
};

export interface Live extends Resources {
  // Whether the event stream is connected; while it is not, data may be late.
  connected: boolean;
  // The last load error, if any.
  error: string | null;
  // Which resources were loaded at least once (an empty list before that
  // means "not known yet", not "none").
  loaded: Partial<Record<Resource, true>>;
  // reload reads a resource again now, e.g. right after a change made here.
  reload: (resource: Resource) => void;
  subscribe: (listener: (e: DaemonEvent) => void) => () => void;
}

const LiveContext = createContext<Live | null>(null);

export function LiveProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<Resources>(EMPTY);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Partial<Record<Resource, true>>>({});
  const listeners = useRef(new Set<(e: DaemonEvent) => void>());
  const timers = useRef(new Map<Resource, ReturnType<typeof setTimeout>>());

  const load = useCallback(async (resource: Resource) => {
    try {
      const value = await LOADERS[resource]();
      setData((d) => ({ ...d, [resource]: value }));
      setLoaded((l) => (l[resource] ? l : { ...l, [resource]: true }));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const scheduleReload = useCallback(
    (resource: Resource) => {
      if (timers.current.has(resource)) return;
      timers.current.set(
        resource,
        setTimeout(() => {
          timers.current.delete(resource);
          void load(resource);
        }, RELOAD_DELAY_MS),
      );
    },
    [load],
  );

  useEffect(() => {
    const pending = timers.current;

    function apply(e: DaemonEvent) {
      switch (e.type) {
        case "hello":
          for (const r of Object.keys(LOADERS) as Resource[]) void load(r);
          break;
        case "agent.status":
          setData((d) => ({
            ...d,
            agents: d.agents.map((a) => (a.address === e.data.address ? { ...a, status: e.data.status } : a)),
          }));
          break;
        case "message.sent":
          setData((d) =>
            d.messages.some((m) => m.id === e.data.id) ? d : { ...d, messages: [...d.messages, e.data].slice(-RECENT_MESSAGES) },
          );
          break;
        case "message.delivered": {
          const ids = new Set(e.data.ids);
          const at = new Date().toISOString();
          setData((d) => ({
            ...d,
            messages: d.messages.map((m) => (ids.has(m.id) && !m.delivered_at ? { ...m, delivered_at: at } : m)),
          }));
          break;
        }
        case "messages.deleted": {
          const ids = new Set(e.data.ids);
          setData((d) => ({ ...d, messages: d.messages.filter((m) => !ids.has(m.id)) }));
          break;
        }
        default:
          for (const r of STALE[e.type] ?? []) scheduleReload(r);
      }

      for (const listener of listeners.current) listener(e);
    }

    const close = connectEvents(apply, setConnected);

    return () => {
      close();
      for (const t of pending.values()) clearTimeout(t);
      pending.clear();
    };
  }, [load, scheduleReload]);

  const subscribe = useCallback((listener: (e: DaemonEvent) => void) => {
    listeners.current.add(listener);
    return () => {
      listeners.current.delete(listener);
    };
  }, []);

  const reload = useCallback((resource: Resource) => void load(resource), [load]);

  const value = useMemo<Live>(
    () => ({ ...data, connected, error, loaded, reload, subscribe }),
    [data, connected, error, loaded, reload, subscribe],
  );

  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLive(): Live {
  const live = useContext(LiveContext);
  if (!live) throw new Error("useLive must be used inside LiveProvider");
  return live;
}

// useDaemonEvents calls listener for every event of the stream, e.g. to
// animate a message or reload what a component shows.
export function useDaemonEvents(listener: (e: DaemonEvent) => void): void {
  const { subscribe } = useLive();
  const latest = useRef(listener);

  useEffect(() => {
    latest.current = listener;
  });

  useEffect(() => subscribe((e) => latest.current(e)), [subscribe]);
}
