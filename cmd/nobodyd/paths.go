package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// stateDirPerm protects the runtime state: it holds the agents' mail.
const stateDirPerm = 0o700

// nobodyHome is the user's global Nobody Inc directory: $NOBODY_HOME, or
// ~/.nobody. It holds the user's templates (catalog/) and the runtime state
// of each project (projects/), see ADR-0004.
func nobodyHome() (string, error) {
	if home := os.Getenv("NOBODY_HOME"); home != "" {
		return filepath.Abs(home)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory (or set NOBODY_HOME): %w", err)
	}

	return filepath.Join(home, ".nobody"), nil
}

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

// resolveProject returns the project to run. With -org, the former
// nobody.yaml is first migrated into its project's .nobody/.
func resolveProject(logger *slog.Logger, opts options) (string, error) {
	if opts.orgPath == "" {
		return opts.projectDir, nil
	}

	projectDir, migrated, err := org.MigrateLegacy(opts.orgPath)
	if err != nil {
		return "", err
	}

	if migrated {
		logger.Info("organization migrated: edit .nobody/ from now on and start with -project",
			"from", opts.orgPath, "to", org.ConfigDir(projectDir))
	} else {
		logger.Warn("the project already has a .nobody/ configuration: the -org file is ignored",
			"ignored", opts.orgPath, "used", org.ConfigDir(projectDir))
	}

	return projectDir, nil
}

// warnIfUntrusted warns when Claude Code does not trust the project yet: each
// agent would then stop on Claude Code's trust question in its terminal.
// Agents still start, as the question can be answered from the UI.
func warnIfUntrusted(logger *slog.Logger, projectDir string) {
	trusted, err := runtime.ProjectTrusted(projectDir)
	if err != nil {
		logger.Warn("cannot check whether Claude Code trusts the project", "project", projectDir, "err", err)

		return
	}

	if !trusted {
		logger.Warn("Claude Code does not trust the project yet: every agent will wait on its trust question. "+
			"Run `claude` once in the project and accept, or answer the question in each agent's terminal.",
			"project", projectDir)
	}
}

// stateDir returns the project's state directory under ~/.nobody/projects/,
// created if needed. It holds what is specific to this machine and never
// shared (ADR-0004): the database and the teams' worktrees.
func stateDir(home, projectDir string) (string, error) {
	dir := projectStateDir(home, projectDir)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return "", fmt.Errorf("create the project state directory: %w", err)
	}

	return dir, nil
}

// projectStateDir is ~/.nobody/projects/<name>-<hash>: the project's folder
// name for humans, and a hash of its absolute path so that two projects with
// the same name (or two clones of one project) never share a database.
func projectStateDir(home, projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))

	return filepath.Join(home, "projects", filepath.Base(projectDir)+"-"+hex.EncodeToString(sum[:4]))
}
