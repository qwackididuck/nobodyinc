// Helpers about scheduled tasks shared by the Schedules tab, the director's
// chat and the office clock (ADR-0012, docs/schedules.md).

import type { RunStatus, Schedule, ScheduleProposal, ScheduleRun, ScheduleView } from "@/lib/api";

// Presets offered in the form: one click fills the recurrence.
export const PRESETS: { label: string; every?: string; cron?: string }[] = [
  { label: "Every 10 min", every: "10m" },
  { label: "Hourly", cron: "0 * * * *" },
  { label: "Daily at 9:00", cron: "0 9 * * *" },
  { label: "Weekdays at 9:00", cron: "0 9 * * 1-5" },
  { label: "Mondays at 9:00", cron: "0 9 * * 1" },
];

// recurrenceText says when a schedule runs, with the preset's name when it
// is one.
export function recurrenceText(s: Pick<Schedule, "every" | "cron">): string {
  if (s.every) return `every ${s.every}`;
  const preset = PRESETS.find((p) => p.cron === s.cron);
  return preset ? preset.label.toLowerCase() : `cron ${s.cron ?? ""}`;
}

export const RUN_LABELS: Record<RunStatus, string> = {
  waiting: "Waiting",
  running: "Running",
  done: "Done",
  canceled: "Canceled",
  skipped: "Skipped",
  unavailable: "No agent",
  interrupted: "Interrupted",
};

// runTone is the color family of an occurrence's status.
export function runTone(s: RunStatus): "ok" | "busy" | "off" | "bad" {
  switch (s) {
    case "done":
      return "ok";
    case "waiting":
    case "running":
      return "busy";
    case "skipped":
    case "canceled":
      return "off";
    case "unavailable":
    case "interrupted":
      return "bad";
  }
}

// isEnded tells an occurrence that is over, done or not.
export function isEnded(r: ScheduleRun): boolean {
  return r.status !== "waiting" && r.status !== "running";
}

// endedAt is when an occurrence ended: skipped ones end when they fall due.
export function endedAt(r: ScheduleRun): string {
  return r.ended_at ?? r.due_at;
}

// alertOf returns what is wrong with a schedule, or null: its last
// occurrence found no agent to run it (the target is gone, and the schedule
// disabled, or none of its agents runs).
export function alertOf(v: ScheduleView): string | null {
  if (v.last_run?.status !== "unavailable") return null;
  return v.enabled
    ? `No agent of ${v.target} is running (on leave or stopped): occurrences are skipped.`
    : `Disabled: ${v.target} can no longer take it. Change the target, then resume.`;
}

// attention counts what waits for the human in the Schedules tab: schedules
// in alert and proposals not decided yet.
export function attention(schedules: ScheduleView[], proposals: ScheduleProposal[]): number {
  return schedules.filter((v) => alertOf(v) !== null).length + proposals.filter((p) => p.status === "proposed").length;
}

// short shows a time as briefly as it stays clear, in the browser's time
// zone: the hour today, the weekday within a week, the date otherwise.
export function short(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  const now = new Date();
  const hour = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (d.toDateString() === now.toDateString()) return hour;
  const days = Math.abs(d.getTime() - now.getTime()) / 86_400_000;
  if (days < 6) return `${d.toLocaleDateString([], { weekday: "short" })} ${hour}`;
  return `${d.toLocaleDateString([], { day: "numeric", month: "short" })} ${hour}`;
}

// when shows a date and time compactly, in the browser's time zone.
export function when(iso: string | undefined): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString([], { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}
