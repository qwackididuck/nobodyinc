"use client";

import { useEffect, useState } from "react";

import { listArchivedTickets, type Ticket } from "@/lib/api";
import { useDaemonEvents, useLive } from "@/lib/live";
import { timeOf } from "../chat/agents";
import { TicketDrawer } from "../kanban/TicketDrawer";
import ui from "../ui/ui.module.css";
import styles from "./review.module.css";

interface Props {
  onClose: () => void;
  onOpenChat: (address: string) => void;
}

// ArchivesPanel is what the librarian keeps: the finished features, newest
// first, with their merge commit and their archived tickets, read-only.
export function ArchivesPanel({ onClose, onOpenChat }: Props) {
  const { features } = useLive();
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [version, setVersion] = useState(0);
  const [open, setOpen] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listArchivedTickets()
      .then((t) => !cancelled && setTickets(t))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, [version]);

  // A feature finished meanwhile brings new tickets to the archives.
  useDaemonEvents((e) => {
    if (e.type === "tickets.changed") setVersion((v) => v + 1);
  });

  const finished = features.filter((f) => f.status === "finished").reverse();

  return (
    <aside className={styles.panel} role="dialog" aria-label="Archives">
      <div className={styles.head}>
        <div>
          <span className={styles.kicker}>Archives</span>
          <h3 className={ui.title}>Finished features</h3>
          <p className={ui.sub}>Filed by the librarian once a feature is merged. Read-only.</p>
        </div>
        <button type="button" className={ui.link} onClick={onClose}>
          Close
        </button>
      </div>
      <div className={styles.body}>
        {error && <p className={ui.error}>{error}</p>}
        {finished.length === 0 && <p className={ui.sub}>The shelves are empty: no feature is finished yet.</p>}
        {finished.map((f) => {
          const own = tickets.filter((t) => t.feature === f.id);
          return (
            <details key={f.id} className={styles.diffFile} open={finished[0]?.id === f.id}>
              <summary>
                {f.id} · {f.title} · {own.length} ticket{own.length === 1 ? "" : "s"}
              </summary>
              <div className={styles.section}>
                <p className={ui.sub}>
                  {f.merge_commit ? (
                    <>
                      Merged into <code>{f.base_branch}</code> as <code>{f.merge_commit.slice(0, 10)}</code>, from <code>{f.branch}</code>.
                    </>
                  ) : (
                    "Finished with nothing to merge."
                  )}
                </p>
                <ul className={styles.commits}>
                  {own.map((t) => (
                    <li key={t.id}>
                      <button type="button" className={ui.link} onClick={() => setOpen(t.id)}>
                        {t.id}
                      </button>{" "}
                      {t.title} <span className={styles.by}>
                        {t.status === "done" ? "done" : "canceled"} · {t.assignee ?? t.team} · {timeOf(t.updated_at)}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            </details>
          );
        })}
      </div>
      {open && <TicketDrawer id={open} readOnly onClose={() => setOpen(null)} onOpenChat={onOpenChat} />}
    </aside>
  );
}
