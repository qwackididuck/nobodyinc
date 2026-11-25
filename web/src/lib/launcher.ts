// Client for the launcher's own routes (ADR-0014, docs/projects.md): the
// known projects, the machine's folders, and the WebSocket through which a
// window holds its project. A project's own API is in api.ts.

import type { Agent, Feature, SpendSummary } from "./api";
import { launcherWS } from "./api";

export type DaemonState = "stopped" | "starting" | "running" | "stopping";

export interface Project {
  id: string;
  name: string;
  path: string;
  // The folder disappeared (moved or deleted outside Nobody Inc).
  missing: boolean;
  git: boolean;
  trusted: boolean;
  // A window holds the project (or held it less than the grace period ago).
  held: boolean;
  state: DaemonState;
  last_opened?: string;
  // Answer of openProject only: the folder has just been set up.
  created?: boolean;
}

export interface Directory {
  name: string;
  path: string;
  git: boolean;
  // Has a .nobody/ directory.
  nobody: boolean;
  // The known project in this folder.
  project?: string;
}

export interface DirectoryListing {
  path: string;
  parent?: string;
  home: string;
  dirs: Directory[];
}

// The unfinished feature that stops a deletion.
export interface UnfinishedFeature {
  id: string;
  title: string;
  branch: string;
}

// DeleteRefused is thrown by deleteProject when a feature is not finished
// (or could not be merged): nothing was deleted.
export class DeleteRefused extends Error {
  constructor(
    message: string,
    readonly feature: UnfinishedFeature,
  ) {
    super(message);
  }
}

export function listProjects(): Promise<Project[]> {
  return request<Project[]>("GET", "/api/projects");
}

// openProject makes a folder a known project, setting it up if it has no
// .nobody/ yet; with create, the folder is created first (and made a git
// repository).
export function openProject(path: string, create = false): Promise<Project> {
  return request<Project>("POST", "/api/projects", { path, create });
}

// deleteProject removes Nobody Inc from a project, never its code. window is
// this window's id when it holds the project; feature says what to do with an
// unfinished feature (otherwise DeleteRefused).
export async function deleteProject(id: string, window: string, feature?: "merge" | "discard"): Promise<void> {
  const params = new URLSearchParams({ window });
  if (feature) params.set("feature", feature);

  const res = await fetch(`/api/projects/${encodeURIComponent(id)}?${params}`, { method: "DELETE" });
  if (res.ok) return;

  const body = (await res.json().catch(() => ({}))) as { error?: string; feature?: UnfinishedFeature };
  if (body.feature) throw new DeleteRefused(body.error ?? res.statusText, body.feature);
  throw new Error(body.error ?? res.statusText);
}

// listDirs lists the sub-folders of path (the home directory by default).
export function listDirs(path?: string): Promise<DirectoryListing> {
  return request<DirectoryListing>("GET", path ? `/api/fs/dirs?path=${encodeURIComponent(path)}` : "/api/fs/dirs");
}

// ProjectActivity is what a project card shows of a running project, read
// from its daemon.
export interface ProjectActivity {
  working: number;
  monthUSD: number;
  feature?: string;
}

// projectActivity reads a running project's agents, spend and active feature
// through the launcher.
export async function projectActivity(id: string): Promise<ProjectActivity> {
  const base = `/p/${encodeURIComponent(id)}`;
  const [agents, spend, features] = await Promise.all([
    request<Agent[]>("GET", `${base}/api/agents`),
    request<SpendSummary>("GET", `${base}/api/spend`),
    request<Feature[]>("GET", `${base}/api/features`),
  ]);

  return {
    working: agents.filter((a) => a.status === "working").length,
    monthUSD: spend.total_usd,
    feature: features.find((f) => f.active)?.title,
  };
}

// UpdateStatus is what the launcher knows about newer releases
// (GET /api/update, docs/updates.md).
export interface UpdateStatus {
  current: string;
  latest?: string;
  url?: string;
  available: boolean;
  checked_at?: string;
  error?: string;
}

// getUpdate reads the result of the last check, the one made at start-up.
export function getUpdate(): Promise<UpdateStatus> {
  return request<UpdateStatus>("GET", "/api/update");
}

// checkUpdate asks GitHub for the latest release now. A failed check comes
// back in error, not as a rejection.
export function checkUpdate(): Promise<UpdateStatus> {
  return request<UpdateStatus>("POST", "/api/update/check");
}

// attachURL is the WebSocket through which window holds the project.
export function attachURL(project: string, window: string, takeover: boolean): string {
  const params = new URLSearchParams({ window });
  if (takeover) params.set("takeover", "true");
  return `${launcherWS()}/api/projects/${encodeURIComponent(project)}/attach?${params}`;
}

// openDesktopWindow asks the desktop app for a new window, on a project or
// on the projects page (ADR-0016).
export async function openDesktopWindow(project?: string): Promise<void> {
  const res = await fetch("/api/windows", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(project ? { project } : {}),
  });
  if (!res.ok) throw new Error(await res.text());
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    cache: "no-store",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const error = (await res.json().catch(() => ({}))) as { error?: string };
    throw new Error(error.error ?? res.statusText);
  }
  return (await res.json()) as T;
}
