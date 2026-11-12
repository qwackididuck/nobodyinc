// Package workspace organizes the agents' work in the project's git
// repository (see docs/workspaces.md).
//
// A feature gets a branch, created from a base branch the user chooses:
//
//	nobody/<feature>            the feature branch: everything validated so far
//	nobody/<feature>--<team>    one branch per team, checked out in the team's
//	                            worktree under <project>/.nobody/worktrees/
//
// Work moves between teams through the feature branch:
//
//   - when an agent claims a ticket, the feature branch is merged into its
//     team's branch (Sync), so it starts from the latest validated work;
//   - when it closes the ticket, its team's branch is merged into the feature
//     branch (Deliver), so the next team gets it.
//
// Nobody Inc never touches any other branch: merging the feature branch where
// they want is the user's decision. All git work goes through the git CLI.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Errors returned to the agent (through the ticket tools), so they say what
// to do instead.
var (
	ErrNotARepository = errors.New("the project directory is not a git repository")
	ErrDirty          = errors.New("there are uncommitted changes in your team's worktree: commit your work before closing the ticket")
	ErrConflict       = errors.New("merging hit a conflict")
)

// BranchPrefix marks the branches Nobody Inc owns.
const BranchPrefix = "nobody/"

// workDirName is where worktrees live, inside the project.
const workDirName = ".nobody"

// Permissions for the exclude file and its directory.
const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// Daemon merges are signed by Nobody Inc, whatever identity the machine has.
var daemonIdentity = []string{"-c", "user.name=Nobody Inc", "-c", "user.email=nobody@nobody.local"}

// Repo is the project the agents work on.
type Repo struct {
	Dir string
}

// Feature is what the workspace needs to know about a feature.
type Feature struct {
	Slug       string // e.g. "add-order-api"
	BaseBranch string // where the feature branch starts from
}

// Branch is the feature branch.
func (f Feature) Branch() string {
	return BranchPrefix + f.Slug
}

// TeamBranch is a team's branch for the feature.
func (f Feature) TeamBranch(team string) string {
	return f.Branch() + "--" + team
}

// Commit is a commit delivered with a ticket.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// TeamDir is the worktree of a team for a feature.
func (r Repo) TeamDir(f Feature, team string) string {
	return filepath.Join(r.Dir, workDirName, "worktrees", f.Slug, team)
}

// CurrentBranch returns the branch the project is on.
func (r Repo) CurrentBranch(ctx context.Context) (string, error) {
	return r.git(ctx, r.Dir, "branch", "--show-current")
}

// Prepare creates the feature branch and the teams' worktrees, if they do not
// exist yet. It is safe to call again (e.g. after a restart).
func (r Repo) Prepare(ctx context.Context, f Feature, teams []string) error {
	if _, err := r.git(ctx, r.Dir, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("%w: %s", ErrNotARepository, r.Dir)
	}

	if err := r.excludeWorkDir(ctx); err != nil {
		return err
	}

	if !r.branchExists(ctx, f.Branch()) {
		if _, err := r.git(ctx, r.Dir, "branch", f.Branch(), f.BaseBranch); err != nil {
			return fmt.Errorf("create feature branch from %q: %w", f.BaseBranch, err)
		}
	}

	for _, team := range teams {
		if err := r.prepareTeam(ctx, f, team); err != nil {
			return err
		}
	}

	return nil
}

// Sync brings the latest validated work (the feature branch) into a team's
// branch. Uncommitted changes in the worktree are kept, as long as git can
// merge around them.
func (r Repo) Sync(ctx context.Context, f Feature, team string) error {
	dir := r.TeamDir(f, team)
	if _, err := r.git(ctx, dir, append(daemonIdentity, "merge", "--no-edit", f.Branch())...); err != nil {
		_, _ = r.git(ctx, dir, "merge", "--abort") // leave the worktree as it was

		return fmt.Errorf("%w while bringing %s into your team's branch: "+
			"commit or set aside your changes, then try again (%w)", ErrConflict, f.Branch(), err)
	}

	return nil
}

// Dirty lists the uncommitted changes in a team's worktree.
func (r Repo) Dirty(ctx context.Context, f Feature, team string) ([]string, error) {
	out, err := r.git(ctx, r.TeamDir(f, team), "status", "--porcelain")
	if err != nil || out == "" {
		return nil, err
	}

	return strings.Split(out, "\n"), nil
}

// Deliver merges a team's branch into the feature branch and returns the
// commits it brought. The feature branch is not checked out anywhere: a
// fast-forward just moves it; otherwise the merge happens in a temporary
// worktree. On a conflict nothing changes and ErrConflict is returned.
func (r Repo) Deliver(ctx context.Context, f Feature, team string) ([]Commit, error) {
	teamBranch := f.TeamBranch(team)

	commits, err := r.commitsBetween(ctx, f.Branch(), teamBranch)
	if err != nil || len(commits) == 0 {
		return nil, err
	}

	if r.isAncestor(ctx, f.Branch(), teamBranch) {
		if _, err := r.git(ctx, r.Dir, "branch", "--force", f.Branch(), teamBranch); err != nil {
			return nil, fmt.Errorf("fast-forward %s: %w", f.Branch(), err)
		}

		return commits, nil
	}

	return commits, r.mergeInto(ctx, f.Branch(), teamBranch)
}

func (r Repo) prepareTeam(ctx context.Context, f Feature, team string) error {
	dir := r.TeamDir(f, team)
	if _, err := os.Stat(dir); err == nil {
		return nil // already there
	}

	args := []string{"worktree", "add", dir, f.TeamBranch(team)}
	if !r.branchExists(ctx, f.TeamBranch(team)) {
		args = []string{"worktree", "add", "-b", f.TeamBranch(team), dir, f.Branch()}
	}

	if _, err := r.git(ctx, r.Dir, args...); err != nil {
		return fmt.Errorf("create worktree of team %s: %w", team, err)
	}

	return nil
}

// mergeInto merges source into target in a throwaway worktree.
func (r Repo) mergeInto(ctx context.Context, target, source string) error {
	tmp := filepath.Join(r.Dir, workDirName, "merge", strings.ReplaceAll(target, "/", "_"))
	if _, err := r.git(ctx, r.Dir, "worktree", "add", tmp, target); err != nil {
		return fmt.Errorf("prepare merge into %s: %w", target, err)
	}

	defer func() {
		_, _ = r.git(context.WithoutCancel(ctx), r.Dir, "worktree", "remove", "--force", tmp) // best-effort cleanup
	}()

	if _, err := r.git(ctx, tmp, append(daemonIdentity, "merge", "--no-edit", source)...); err != nil {
		_, _ = r.git(ctx, tmp, "merge", "--abort")

		return fmt.Errorf("%w while merging %s into %s: bring %s into your branch "+
			"(git merge %s), resolve the conflict, commit, then close the ticket again", ErrConflict, source, target, target, target)
	}

	return nil
}

// commitsBetween lists the commits in to that are not in from, oldest first.
func (r Repo) commitsBetween(ctx context.Context, from, to string) ([]Commit, error) {
	out, err := r.git(ctx, r.Dir, "log", "--reverse", "--format=%H%x09%s", from+".."+to)
	if err != nil || out == "" {
		return nil, err
	}

	var commits []Commit

	for line := range strings.SplitSeq(out, "\n") {
		hash, subject, _ := strings.Cut(line, "\t")
		commits = append(commits, Commit{Hash: hash, Subject: subject})
	}

	return commits, nil
}

func (r Repo) isAncestor(ctx context.Context, ancestor, of string) bool {
	_, err := r.git(ctx, r.Dir, "merge-base", "--is-ancestor", ancestor, of)

	return err == nil
}

func (r Repo) branchExists(ctx context.Context, branch string) bool {
	_, err := r.git(ctx, r.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)

	return err == nil
}

// excludeWorkDir keeps .nobody/ out of the user's git status, without
// touching their .gitignore.
func (r Repo) excludeWorkDir(ctx context.Context) error {
	path, err := r.git(ctx, r.Dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Dir, path)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if bytes.Contains(existing, []byte("/"+workDirName+"/")) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(f, "\n# Nobody Inc worktrees\n/%s/\n", workDirName)

	return errors.Join(err, f.Close())
}

// git runs a git command in dir and returns its trimmed output.
func (r Repo) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(string(out)), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// maxSlugLength keeps branch and directory names short.
const maxSlugLength = 40

// Slugify turns a feature title into a branch-friendly name.
func Slugify(title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > maxSlugLength {
		slug = strings.TrimRight(slug[:maxSlugLength], "-")
	}

	if slug == "" {
		return "feature"
	}

	return slug
}
