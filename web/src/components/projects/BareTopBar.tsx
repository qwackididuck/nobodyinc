"use client";

import { checkNow, useUpdate } from "@/lib/update";
import { BrandMark } from "../shell/TopBar";
import shell from "../shell/shell.module.css";
import ui from "../ui/ui.module.css";
import { ProjectMenu } from "./ProjectMenu";
import styles from "./projects.module.css";

interface Props {
  // lobby: the projects page, which shows no project.
  lobby?: boolean;
}

// BareTopBar is the top bar without the project's tabs: on the projects
// page, and in a project window while its control room cannot be shown
// (opening, open in another window).
export function BareTopBar({ lobby = false }: Props) {
  const updates = useUpdate();
  return (
    <header className={shell.topbar}>
      <div className={shell.brand}>
        <BrandMark />
        Nobody Inc
      </div>
      {lobby ? (
        <>
          <nav className={shell.tabs} role="tablist" aria-label="Views">
            <button type="button" role="tab" aria-selected="true" className={shell.tab}>
              Projects
            </button>
          </nav>
          <button type="button" className={`${ui.btn} ${styles.pushRight}`} disabled={updates.checking} onClick={() => void checkNow()}>
            Check for updates
          </button>
        </>
      ) : (
        <ProjectMenu />
      )}
    </header>
  );
}
