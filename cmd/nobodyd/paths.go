package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// userClaudeMD is the user's own CLAUDE.md, whose text the agents get
// (ADR-0008): in $CLAUDE_CONFIG_DIR when set, as Claude Code does, otherwise
// in ~/.claude. "" when the home directory cannot be found.
func userClaudeMD() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "CLAUDE.md")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".claude", "CLAUDE.md")
}

// trustNobodyHome makes Claude Code trust the daemon's own directory
// (~/.nobody), before any agent starts: every agent gets the staging
// directory of the personal skills, inside it, with --add-dir, and Claude
// Code would otherwise ask at every session start whether to trust it
// (ADR-0011, runtime.Trust). A failure is only a warning: agents then wait on
// that question in their terminal. The directory exists by then (the
// project's state directory is inside it).
func trustNobodyHome(logger *slog.Logger, home string) {
	changed, err := runtime.Trust(home)
	if err != nil {
		logger.Warn("cannot make Claude Code trust the Nobody Inc directory: agents will ask whether to trust the personal skills",
			"dir", home, "err", err)

		return
	}

	if changed {
		logger.Info("Claude Code now trusts the Nobody Inc directory, where the agents' personal skills are linked", "dir", home)
	}
}

// warnIfUntrusted warns when Claude Code does not trust the project: each
// agent would then stop on Claude Code's trust question in its terminal.
// Opening a project from the control room trusts it (projects.SetUp), but
// the trust can be lost since. Agents still start, as the question can be
// answered from the UI.
func warnIfUntrusted(logger *slog.Logger, projectDir string) {
	trusted, err := runtime.ProjectTrusted(projectDir)
	if err != nil {
		logger.Warn("cannot check whether Claude Code trusts the project", "project", projectDir, "err", err)

		return
	}

	if !trusted {
		logger.Warn("Claude Code does not trust the project: every agent will wait on its trust question. "+
			"Open the project again from the control room, or answer the question in each agent's terminal.",
			"project", projectDir)
	}
}
