package workspace

// Reviewing and finishing a feature (ADR-0007): the human reads what the
// feature branch brings compared with its base branch, then finishes it,
// which merges the feature branch into the base branch, locally, and removes
// the teams' worktrees and branches. This is the only time Nobody Inc writes
// to a branch it does not own, and only on the human's request. It never
// pushes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Errors returned to the human when a feature cannot be finished. Nothing is
// changed when they occur.
var (
	ErrBaseDirty     = errors.New("the base branch is checked out in the project with uncommitted changes")
	ErrBaseElsewhere = errors.New("the base branch is checked out in another worktree")
	ErrBaseConflict  = errors.New("merging the feature into its base branch hits a conflict")
	ErrNoBaseBranch  = errors.New("the base branch does not exist")
)

// ReviewCommit is a commit of the feature branch.
type ReviewCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"` // ISO 8601
	Subject string `json:"subject"`
}

// FileChange is a file the feature changes.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // A, M, D, R
	Added   int    `json:"added"`  // -1 for a binary file
	Deleted int    `json:"deleted"`
}

// Review is what the feature branch brings compared with its base branch,
// since they diverged.
type Review struct {
	Commits   []ReviewCommit
	Files     []FileChange
	Diff      string
	Truncated bool // Diff was cut at the requested size
}

// Review describes what the feature branch brings to its base branch. The
// diff is cut at maxDiff bytes.
func (r Repo) Review(ctx context.Context, f Feature, maxDiff int) (Review, error) {
	if !r.branchExists(ctx, f.BaseBranch) {
		return Review{}, fmt.Errorf("%w: %s", ErrNoBaseBranch, f.BaseBranch)
	}

	commits, err := r.reviewCommits(ctx, f)
	if err != nil {
		return Review{}, err
	}

	files, err := r.fileChanges(ctx, f)
	if err != nil {
		return Review{}, err
	}

	diff, err := r.git(ctx, r.Dir, "diff", "-M", f.BaseBranch+"..."+f.Branch())
	if err != nil {
		return Review{}, err
	}

	review := Review{Commits: commits, Files: files, Diff: diff}
	if len(diff) > maxDiff {
		review.Diff, review.Truncated = diff[:maxDiff], true
	}

	return review, nil
}

func (r Repo) reviewCommits(ctx context.Context, f Feature) ([]ReviewCommit, error) {
	out, err := r.git(ctx, r.Dir, "log", "--reverse", "--no-merges", "--format=%H%x09%an%x09%aI%x09%s", f.BaseBranch+".."+f.Branch())
	if err != nil || out == "" {
		return []ReviewCommit{}, err
	}

	// One line per commit: hash, author, date and subject, tab-separated.
	const fields = 4

	var commits []ReviewCommit

	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "\t", fields)
		if len(parts) == fields {
			commits = append(commits, ReviewCommit{Hash: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3]})
		}
	}

	return commits, nil
}

// fileChanges combines `git diff --name-status` (what happened to each file)
// with `--numstat` (how many lines).
func (r Repo) fileChanges(ctx context.Context, f Feature) ([]FileChange, error) {
	span := f.BaseBranch + "..." + f.Branch()

	statuses, err := r.git(ctx, r.Dir, "diff", "-M", "--name-status", span)
	if err != nil {
		return nil, err
	}

	numbers, err := r.git(ctx, r.Dir, "diff", "-M", "--numstat", span)
	if err != nil {
		return nil, err
	}

	files := []FileChange{}

	for line := range strings.SplitSeq(statuses, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		// A rename is "R<score>\told\tnew": keep the new path.
		files = append(files, FileChange{Path: parts[len(parts)-1], Status: parts[0][:1]})
	}

	counts := map[string][2]int{}

	for line := range strings.SplitSeq(numbers, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		added, err := strconv.Atoi(parts[0])
		if err != nil {
			added = -1 // "-" for a binary file
		}

		deleted, err := strconv.Atoi(parts[1])
		if err != nil {
			deleted = 0 // "-" for a binary file
		}

		counts[renamedPath(parts[len(parts)-1])] = [2]int{added, deleted}
	}

	for i := range files {
		c := counts[files[i].Path]
		files[i].Added, files[i].Deleted = c[0], c[1]
	}

	return files, nil
}

// renamedPath turns numstat's "dir/{old => new}.go" or "old => new" into the
// new path.
func renamedPath(p string) string {
	if !strings.Contains(p, " => ") {
		return p
	}

	if open := strings.Index(p, "{"); open >= 0 {
		end := strings.Index(p, "}")
		_, newPart, _ := strings.Cut(p[open+1:end], " => ")

		return filepath.Clean(p[:open] + newPart + p[end+1:])
	}

	_, newPath, _ := strings.Cut(p, " => ")

	return newPath
}

// Finish merges the feature branch into its base branch with a merge commit
// signed by Nobody Inc, and returns that commit ("" when the feature brings
// nothing). Where the merge happens depends on where the base branch is:
//   - checked out in the project: there, if no tracked file has uncommitted
//     changes (ErrBaseDirty otherwise);
//   - checked out in another worktree: refused (ErrBaseElsewhere);
//   - not checked out: in a throwaway worktree.
//
// On a conflict the merge is aborted and ErrBaseConflict returned: nothing
// changes.
func (r Repo) Finish(ctx context.Context, f Feature, message string) (string, error) {
	if !r.branchExists(ctx, f.BaseBranch) {
		return "", fmt.Errorf("%w: %s", ErrNoBaseBranch, f.BaseBranch)
	}

	if r.isAncestor(ctx, f.Branch(), f.BaseBranch) {
		return "", nil // everything is already in the base branch
	}

	where, err := r.checkedOutAt(ctx, f.BaseBranch)
	if err != nil {
		return "", err
	}

	switch {
	case where == "":
		err = r.mergeInTemporaryWorktree(ctx, f, message)
	case samePath(where, r.Dir):
		err = r.mergeInProject(ctx, f, message)
	default:
		err = fmt.Errorf("%w (%s): finish the feature from there, or switch that worktree to another branch", ErrBaseElsewhere, where)
	}

	if err != nil {
		return "", err
	}

	return r.git(ctx, r.Dir, "rev-parse", f.BaseBranch)
}

func (r Repo) mergeInProject(ctx context.Context, f Feature, message string) error {
	dirty, err := r.git(ctx, r.Dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}

	if dirty != "" {
		return fmt.Errorf("%w: commit or stash them, then finish again:\n%s", ErrBaseDirty, dirty)
	}

	return r.mergeNoFF(ctx, r.Dir, f, message)
}

func (r Repo) mergeInTemporaryWorktree(ctx context.Context, f Feature, message string) error {
	if err := r.checkWorktreesDir(); err != nil {
		return err
	}

	tmp := filepath.Join(r.WorktreesDir, mergeDirName, strings.ReplaceAll(f.BaseBranch, "/", "_"))
	if _, err := r.git(ctx, r.Dir, "worktree", "add", tmp, f.BaseBranch); err != nil {
		return fmt.Errorf("prepare the merge into %s: %w", f.BaseBranch, err)
	}

	defer func() {
		_, _ = r.git(context.WithoutCancel(ctx), r.Dir, "worktree", "remove", "--force", tmp) // best-effort cleanup
	}()

	return r.mergeNoFF(ctx, tmp, f, message)
}

// mergeNoFF merges the feature branch into what dir has checked out, always
// with a merge commit, so that the feature stays visible in the history.
func (r Repo) mergeNoFF(ctx context.Context, dir string, f Feature, message string) error {
	if _, err := r.git(ctx, dir, append(daemonIdentity, "merge", "--no-ff", "-m", message, f.Branch())...); err != nil {
		_, _ = r.git(ctx, dir, "merge", "--abort") // back to where it was

		return fmt.Errorf("%w: nothing was changed. Merge it yourself and resolve the conflict:\n"+
			"  git switch %s && git merge %s", ErrBaseConflict, f.BaseBranch, f.Branch())
	}

	return nil
}

// checkedOutAt returns the worktree that has branch checked out, or "".
func (r Repo) checkedOutAt(ctx context.Context, branch string) (string, error) {
	out, err := r.git(ctx, r.Dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}

	var path string

	for line := range strings.SplitSeq(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		}

		if line == "branch refs/heads/"+branch {
			return path, nil
		}
	}

	return "", nil
}

func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)

	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}

	return ra == rb
}

// Cleanup removes the teams' worktrees of a finished feature and their
// branches. Uncommitted changes in them are lost. The feature branch is
// kept: it holds the work, merged or not.
func (r Repo) Cleanup(ctx context.Context, f Feature, teams []string) error {
	var errs []error

	for _, team := range teams {
		if dir := r.TeamDir(f, team); dirExists(dir) {
			if _, err := r.git(ctx, r.Dir, "worktree", "remove", "--force", dir); err != nil {
				errs = append(errs, err)
			}
		}

		if r.branchExists(ctx, f.TeamBranch(team)) {
			if _, err := r.git(ctx, r.Dir, "branch", "-D", f.TeamBranch(team)); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if _, err := r.git(ctx, r.Dir, "worktree", "prune"); err != nil {
		errs = append(errs, err)
	}

	// The feature's directory is empty once its worktrees are gone.
	if r.WorktreesDir != "" {
		_ = os.Remove(filepath.Join(r.WorktreesDir, f.Slug)) // best effort: left alone if something else is in it
	}

	return errors.Join(errs...)
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)

	return err == nil && info.IsDir()
}
