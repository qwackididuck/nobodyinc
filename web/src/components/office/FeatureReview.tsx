"use client";

import { useEffect, useState } from "react";

import { type Feature, type FeatureReview as Review, finishFeature, reviewFeature } from "@/lib/api";
import { useDaemonEvents, useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import styles from "./review.module.css";

interface Props {
  feature: Feature;
  onClose: () => void;
}

// FeatureReview is what the human reads before finishing the active feature:
// the commits, the files changed and the diff against the base branch, and
// the tickets not done yet. "Merge and finish" merges the feature into its
// base branch, locally (never pushed), and ends it (ADR-0007).
export function FeatureReview({ feature, onClose }: Props) {
  const { tickets } = useLive();
  const [review, setReview] = useState<Review | null>(null);
  const [version, setVersion] = useState(0);
  const [force, setForce] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<Feature | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Expanded, the review fills the whole window, for reading the diff; Escape reduces it.
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    if (!expanded) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setExpanded(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [expanded]);

  useEffect(() => {
    let cancelled = false;
    reviewFeature(feature.id)
      .then((r) => !cancelled && setReview(r))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, [feature.id, version]);

  // New deliveries or ticket changes update the review.
  useDaemonEvents((e) => {
    if (!done && (e.type === "tickets.changed" || (e.type === "features.changed" && e.data.id === feature.id))) setVersion((v) => v + 1);
  });

  async function finish() {
    setBusy(true);
    try {
      setDone(await finishFeature(feature.id, force));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  }

  const unfinished = review?.unfinished_tickets ?? [];
  const ticketCount = tickets.filter((t) => t.feature === feature.id).length;
  const blocked = unfinished.length > 0 && !force;

  return (
    <aside className={expanded ? `${styles.panel} ${styles.expanded}` : styles.panel} role="dialog" aria-label={`Review ${feature.id}`}>
      <div className={styles.head}>
        <div>
          <span className={styles.kicker}>Review · {feature.id}</span>
          <h3 className={ui.title}>{feature.title}</h3>
          <p className={ui.sub}>
            <code>{feature.branch}</code> into <code>{feature.base_branch}</code>
          </p>
        </div>
        <div className={styles.headActions}>
          <button type="button" className={ui.link} onClick={() => setExpanded((e) => !e)} aria-pressed={expanded}>
            {expanded ? "Reduce (Esc)" : "Expand"}
          </button>
          <button type="button" className={ui.link} onClick={onClose}>
            Close
          </button>
        </div>
      </div>

      {done ? (
        <div className={styles.section}>
          <p className={ui.note}>
            {done.merge_commit ? `Merged into ${done.base_branch} as ${done.merge_commit.slice(0, 10)}.` : "There was nothing to merge."} The feature is
            finished: the teams are back in the project and their worktrees are being removed. Nothing was pushed.
          </p>
          <button type="button" className={ui.btn} onClick={onClose}>
            Back to the office
          </button>
        </div>
      ) : (
        <>
          {!review && !error && <p className={ui.sub}>Loading…</p>}
          {review && (
            <div className={styles.body}>
              {unfinished.length > 0 && (
                <section className={styles.warning}>
                  <strong>
                    {unfinished.length} ticket{unfinished.length > 1 ? "s are" : " is"} not done yet
                  </strong>
                  <ul>
                    {unfinished.map((t) => (
                      <li key={t.id}>
                        {t.id} · {t.title} · {t.status === "in_progress" ? `in progress (${t.assignee ?? "?"})` : "open"}
                      </li>
                    ))}
                  </ul>
                </section>
              )}

              <section className={styles.section}>
                <h4 className={styles.kicker}>
                  {review.commits.length} commit{review.commits.length === 1 ? "" : "s"}
                </h4>
                {review.commits.length === 0 && <p className={ui.sub}>The feature brings nothing yet.</p>}
                <ol className={styles.commits}>
                  {review.commits.map((c) => (
                    <li key={c.hash}>
                      <code>{c.hash.slice(0, 7)}</code> {c.subject} <span className={styles.by}>{c.author}</span>
                    </li>
                  ))}
                </ol>
              </section>

              <section className={styles.section}>
                <h4 className={styles.kicker}>
                  {review.files.length} file{review.files.length === 1 ? "" : "s"} changed
                </h4>
                <ul className={styles.files}>
                  {review.files.map((f) => (
                    <li key={f.path}>
                      <span className={styles.status} data-status={f.status}>
                        {f.status}
                      </span>
                      <a href={`#diff-${encodeURIComponent(f.path)}`}>{f.path}</a>
                      <span className={styles.counts}>
                        {f.added < 0 ? (
                          "binary"
                        ) : (
                          <>
                            <span className={styles.plus}>+{f.added}</span> <span className={styles.minus}>−{f.deleted}</span>
                          </>
                        )}
                      </span>
                    </li>
                  ))}
                </ul>
              </section>

              <section className={styles.section}>
                <h4 className={styles.kicker}>Diff</h4>
                {splitDiff(review.diff).map((file) => (
                  <details key={file.path} id={`diff-${encodeURIComponent(file.path)}`} className={styles.diffFile} open>
                    <summary>{file.path}</summary>
                    <pre className={styles.diff}>
                      {file.lines.map((line, i) => (
                        <span key={i} className={lineClass(line)}>
                          {line}
                          {"\n"}
                        </span>
                      ))}
                    </pre>
                  </details>
                ))}
                {review.diff_truncated && <p className={ui.sub}>The diff is cut at 400 KB: read the rest with git diff.</p>}
              </section>
            </div>
          )}

          <div className={styles.foot}>
            <p className={styles.by}>
              Finishing archives the feature&apos;s {ticketCount} ticket{ticketCount === 1 ? "" : "s"}: they leave the Kanban and go to the
              archives, where they stay readable.
            </p>
            {unfinished.length > 0 && (
              <label className={styles.force}>
                <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} /> Finish anyway: these tickets are canceled and archived, and what they did not deliver is lost
              </label>
            )}
            {confirming ? (
              <div className={styles.actions}>
                <span>
                  Merge into <code>{feature.base_branch}</code> on this machine and end the feature?
                </span>
                <button type="button" className={ui.btnPrimary} disabled={busy} onClick={() => void finish()}>
                  Merge and finish
                </button>
                <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
                  Cancel
                </button>
              </div>
            ) : (
              <div className={styles.actions}>
                <button type="button" className={`${ui.btnPrimary} ${ui.big}`} disabled={!review || blocked} onClick={() => setConfirming(true)}>
                  Merge into {feature.base_branch} and finish
                </button>
                <span className={styles.by}>Local merge, never pushed.</span>
              </div>
            )}
            {error && <pre className={styles.error}>{error}</pre>}
          </div>
        </>
      )}
    </aside>
  );
}

// splitDiff cuts a unified diff into one part per file.
function splitDiff(diff: string): { path: string; lines: string[] }[] {
  const files: { path: string; lines: string[] }[] = [];
  for (const line of diff.split("\n")) {
    const header = /^diff --git a\/.* b\/(.*)$/.exec(line);
    if (header) {
      files.push({ path: header[1], lines: [] });
      continue;
    }
    files.at(-1)?.lines.push(line);
  }
  return files;
}

function lineClass(line: string): string | undefined {
  if (line.startsWith("+++") || line.startsWith("---") || line.startsWith("index ")) return styles.meta;
  if (line.startsWith("@@")) return styles.hunk;
  if (line.startsWith("+")) return styles.add;
  if (line.startsWith("-")) return styles.del;
  return undefined;
}
