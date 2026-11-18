"use client";

import { useEffect } from "react";

import { useLive } from "@/lib/live";
import { useOpenSchedules } from "../schedules/nav";
import { endedAt, isEnded, RUN_LABELS, runTone, when } from "../schedules/scheduling";
import sched from "../schedules/schedules.module.css";
import ui from "../ui/ui.module.css";
import styles from "./review.module.css";

interface Props {
  onClose: () => void;
  // onSeen records the end of the latest occurrence shown, as seen.
  onSeen: (at: string) => void;
}

// ClockPanel is what the office clock tells: the scheduled tasks' latest
// occurrences that are over, done or skipped, newest first. One opens its
// schedule in the Schedules tab.
export function ClockPanel({ onClose, onSeen }: Props) {
  const { scheduleRuns, schedules } = useLive();
  const openSchedules = useOpenSchedules();

  const ended = scheduleRuns.filter(isEnded).sort((a, b) => endedAt(b).localeCompare(endedAt(a)));
  const latest = ended[0] ? endedAt(ended[0]) : undefined;

  useEffect(() => {
    if (latest) onSeen(latest);
  }, [latest, onSeen]);

  return (
    <aside className={styles.panel} role="dialog" aria-label="Scheduled tasks done or skipped">
      <div className={styles.head}>
        <div>
          <span className={styles.kicker}>The clock</span>
          <h3 className={ui.title}>Scheduled tasks, lately</h3>
          <p className={ui.sub}>Occurrences that are over: done, skipped, or with no agent to run them.</p>
        </div>
        <button type="button" className={ui.link} onClick={onClose}>
          Close
        </button>
      </div>
      <div className={styles.body}>
        <div className={styles.section}>
          {ended.length === 0 && <p className={ui.sub}>Nothing yet: no scheduled task has run.</p>}
          <ul className={styles.commits}>
            {ended.map((r) => {
              const exists = schedules.some((s) => s.id === r.schedule_id);
              return (
                <li key={r.id}>
                  <span className={sched.runStatus} data-tone={runTone(r.status)}>
                    {RUN_LABELS[r.status]}
                  </span>{" "}
                  {exists ? (
                    <button type="button" className={ui.link} onClick={() => openSchedules({ kind: "schedule", id: r.schedule_id })}>
                      {r.schedule_title}
                    </button>
                  ) : (
                    <span title="This schedule was deleted">{r.schedule_title}</span>
                  )}{" "}
                  <span className={styles.by}>
                    {when(endedAt(r))}
                    {r.agent && ` · ${r.agent}`}
                    {r.ticket && ` · ${r.ticket}`}
                    {r.trigger === "manual" && " · run now"}
                    {r.note && ` · ${r.note}`}
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      </div>
    </aside>
  );
}
