// Package projects knows where Nobody Inc keeps the projects it runs, and how
// a folder becomes one (ADR-0014, docs/projects.md).
//
// A project is a folder holding a .nobody/ directory, committed with the code
// (ADR-0004). What is specific to this machine lives outside it, in the
// user's Nobody Inc directory:
//
//	~/.nobody/                  Home ($NOBODY_HOME replaces it)
//	  settings.yaml             the user's settings, for every project
//	  catalog/                  the user's agent templates
//	  projects.yaml             the registry of known projects (Registry)
//	  projects/<id>/            one project's state (StateDir): database,
//	                            worktrees, MCP and skills staging, daemon.lock
//
// The launcher uses this package to list, set up and delete projects; the
// project daemon uses it to find its state directory and lock it, so that a
// project never runs twice.
package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// stateDirPerm protects the runtime state: it holds the agents' mail.
const stateDirPerm = 0o700

// Home is the user's Nobody Inc directory: $NOBODY_HOME, or ~/.nobody.
func Home() (string, error) {
	if home := os.Getenv("NOBODY_HOME"); home != "" {
		return filepath.Abs(home)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory (or set NOBODY_HOME): %w", err)
	}

	return filepath.Join(home, ".nobody"), nil
}

// ID is a project's identifier: its folder's name for humans, and a hash of
// its absolute path so that two projects with the same name (or two clones
// of one project) never share a state directory. projectDir must be
// absolute.
func ID(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))

	return filepath.Base(projectDir) + "-" + hex.EncodeToString(sum[:4])
}

// StateDir is a project's state directory, home/projects/<id>: what is
// specific to this machine and never shared (ADR-0004).
func StateDir(home, projectDir string) string {
	return filepath.Join(home, "projects", ID(projectDir))
}

// EnsureStateDir returns the project's state directory, created if needed.
func EnsureStateDir(home, projectDir string) (string, error) {
	dir := StateDir(home, projectDir)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return "", fmt.Errorf("create the project state directory: %w", err)
	}

	return dir, nil
}
