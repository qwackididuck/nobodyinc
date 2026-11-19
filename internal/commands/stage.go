package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/gitfiles"
)

// StagedCommandsDir is where Stage puts the links, inside the staging
// directory: Claude Code loads <dir>/.claude/commands of a directory given
// with --add-dir, through symbolic links to the command files (checked by
// test/contract).
func StagedCommandsDir(stagingDir string) string {
	return filepath.Join(stagingDir, ".claude", "commands")
}

// Stage makes the staging directory hold one symbolic link per personal
// command agents get, at the same path as under the personal commands
// directory (sub-directories are real directories), and nothing else: links
// to commands now gone or shadowed are removed, and the directories they
// leave empty. Every agent is started with --add-dir on stagingDir, the same
// directory as for the personal skills (ADR-0011).
//
// Links rather than copies: a change of a personal command reaches the agents
// at their next start without copying again. Agents cannot write through
// them, as they are denied writes to the staging and personal directories
// (runtime.protectedRules).
func Stage(stagingDir, personalDir string, personal []Command) error {
	root := StagedCommandsDir(stagingDir)
	if err := os.MkdirAll(root, dirMode); err != nil {
		return fmt.Errorf("staging directory for personal commands: %w", err)
	}

	want := make(map[string]string, len(personal)) // staged path -> target
	for _, c := range personal {
		rel, err := filepath.Rel(personalDir, c.Path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("stage personal command %s: not under %s", c.Name, personalDir)
		}

		want[filepath.Join(root, rel)] = c.Path
	}

	if err := removeUnwanted(root, want); err != nil {
		return fmt.Errorf("staging directory for personal commands: %w", err)
	}

	for link, target := range want {
		if err := os.MkdirAll(filepath.Dir(link), dirMode); err != nil {
			return fmt.Errorf("stage personal command %s: %w", link, err)
		}

		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("stage personal command %s: %w", link, err)
		}
	}

	return nil
}

// removeUnwanted removes, under root, everything that is not a wanted link
// to its target, then the directories left empty. The links already in
// place are dropped from want. Removals go through an os.Root, so that
// nothing outside root is ever removed.
func removeUnwanted(root string, want map[string]string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()

	var dirs []string

	err = fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || rel == "." {
			return err
		}

		if d.IsDir() {
			dirs = append(dirs, rel)

			return nil
		}

		path := filepath.Join(root, filepath.FromSlash(rel))
		if target, ok := want[path]; ok && linksTo(path, target) {
			delete(want, path) // already in place

			return nil
		}

		return r.Remove(rel)
	})
	if err != nil {
		return err
	}

	// Deepest first, so that a parent emptied by its children goes too.
	for _, dir := range slices.Backward(dirs) {
		_ = r.Remove(dir) // fails on a directory still holding links: kept
	}

	return nil
}

// linksTo reports whether path is a symbolic link to target.
func linksTo(path, target string) bool {
	got, err := os.Readlink(path)
	if errors.Is(err, fs.ErrInvalid) {
		return false // not a link
	}

	return err == nil && got == target
}

// Uncommitted returns the names of the project commands that are not
// committed as they are: untracked, or changed since the last commit. Agents
// working on a feature only see what is committed (docs/workspaces.md), so
// the control room warns about these.
func Uncommitted(ctx context.Context, projectDir string, found Found) ([]string, error) {
	const dir = ".claude/commands"

	state, err := gitfiles.Of(ctx, projectDir, dir)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, c := range found.Commands {
		if c.Scope != ScopeProject {
			continue
		}

		rel, err := filepath.Rel(projectDir, c.Path)
		if err != nil {
			return nil, err
		}

		if !state.Committed(filepath.ToSlash(rel)) {
			out = append(out, c.Name)
		}
	}

	return out, nil
}
