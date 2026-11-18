package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Forget removes Nobody Inc from a project, never its code (ADR-0014): the
// project's .nobody/ (a tracked deletion when it was committed, left to the
// user to commit) and its state directory (database, worktrees, MCP and
// skills staging). The teams' worktrees must be removed first
// (RemoveWorktrees), so that git does not keep them registered; the git
// branches all stay.
func Forget(home, projectDir string) error {
	if !filepath.IsAbs(projectDir) {
		return fmt.Errorf("forget %q: the project path must be absolute", projectDir)
	}

	if err := os.RemoveAll(org.ConfigDir(projectDir)); err != nil {
		return fmt.Errorf("remove the project's configuration: %w", err)
	}

	if err := os.RemoveAll(StateDir(home, projectDir)); err != nil {
		return fmt.Errorf("remove the project's state: %w", err)
	}

	return nil
}

// RemoveWorktrees removes the git worktrees Nobody Inc made for the
// project's teams, the ones under <state>/worktrees, even with uncommitted
// changes, which are lost. Their branches stay. A project that is not a git
// repository has none.
func RemoveWorktrees(ctx context.Context, home, projectDir string) error {
	if !IsGitRepo(projectDir) {
		return nil
	}

	ours, err := worktreesRoot(home, projectDir)
	if err != nil || ours == "" {
		return err
	}

	listed, err := git(ctx, projectDir, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}

	for line := range strings.Lines(listed) {
		path, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree ")
		if !ok || !strings.HasPrefix(path, ours+string(filepath.Separator)) {
			continue
		}

		// Twice --force: also removes a locked worktree, or one with changes.
		if _, err := git(ctx, projectDir, "worktree", "remove", "--force", "--force", path); err != nil {
			return err
		}
	}

	_, err = git(ctx, projectDir, "worktree", "prune")

	return err
}

// worktreesRoot is the real path (symbolic links resolved, as git lists
// them) of the project's worktrees directory; "" when there is none.
func worktreesRoot(home, projectDir string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(StateDir(home, projectDir), "worktrees"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	return root, err
}

// git runs a git command in dir and returns its output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
		}

		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(out), nil
}
