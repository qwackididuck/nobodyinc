"use client";

import { BrandMark } from "../shell/TopBar";
import shell from "../shell/shell.module.css";
import { ProjectMenu } from "./ProjectMenu";

interface Props {
  // lobby: the projects page, which shows no project.
  lobby?: boolean;
}

// BareTopBar is the top bar without the project's tabs: on the projects
// page, and in a project window while its control room cannot be shown
// (opening, open in another window).
export function BareTopBar({ lobby = false }: Props) {
  return (
    <header className={shell.topbar}>
      <div className={shell.brand}>
        <BrandMark />
        Nobody Inc
      </div>
      {lobby ? (
        <nav className={shell.tabs} role="tablist" aria-label="Views">
          <button type="button" role="tab" aria-selected="true" className={shell.tab}>
            Projects
          </button>
        </nav>
      ) : (
        <ProjectMenu />
      )}
    </header>
  );
}
