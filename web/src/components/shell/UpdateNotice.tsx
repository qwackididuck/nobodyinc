"use client";

import { useEffect } from "react";

import { clearOutcome, dismissRelease, useUpdate, watchUpdate } from "@/lib/update";
import ui from "../ui/ui.module.css";
import styles from "./update.module.css";

// Release pages live on GitHub: a link to anything else is not shown.
const RELEASE_PREFIX = "https://github.com/";

// How long the answer to "Check for updates" stays on screen.
const OUTCOME_MS = 6_000;

// UpdateNotice tells the human that a newer release exists (docs/updates.md),
// and answers "Check for updates". It sits in a corner of the window, so that
// it is the same on the projects page and in a project, and covers nothing
// permanent. Nothing is downloaded or installed: it links to the release page.
export function UpdateNotice() {
  const { status, outcome, dismissed } = useUpdate();

  useEffect(() => watchUpdate(), []);

  useEffect(() => {
    if (!outcome) return;
    const timer = setTimeout(clearOutcome, OUTCOME_MS);
    return () => clearTimeout(timer);
  }, [outcome]);

  const release = status?.available && status.latest && status.latest !== dismissed ? status : null;

  if (outcome) {
    return (
      <div className={styles.notice} role="status" data-kind={outcome.kind}>
        <span className={styles.text}>
          {outcome.kind === "uptodate" && <>Nobody Inc is up to date ({outcome.current}).</>}
          {outcome.kind === "dev" && <>This is a development build: it is not compared with the latest release.</>}
          {outcome.kind === "error" && <>Could not check for updates: {outcome.message}</>}
        </span>
        <button type="button" className={ui.btn} onClick={clearOutcome}>
          Close
        </button>
      </div>
    );
  }

  if (!release?.latest) return null;

  const link = release.url?.startsWith(RELEASE_PREFIX) ? release.url : null;

  return (
    <div className={styles.notice} role="status" data-kind="available">
      <span className={styles.text}>
        <strong>{release.latest}</strong> is available (this is {release.current}).
      </span>
      {link && (
        <a className={ui.btnPrimary} href={link} target="_blank" rel="noreferrer noopener">
          Release notes
        </a>
      )}
      <button type="button" className={ui.btn} onClick={() => dismissRelease(release.latest ?? "")}>
        Later
      </button>
    </div>
  );
}
