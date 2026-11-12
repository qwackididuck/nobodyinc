package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var identity = []string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}

// newRepo creates a git repository with one commit on "main".
func newRepo(t *testing.T) Repo {
	t.Helper()

	dir := t.TempDir()
	run(t, dir, "init", "--initial-branch=main")
	writeFile(t, dir, "README.md", "hello\n")
	run(t, dir, "add", ".")
	run(t, dir, append(identity, "commit", "-m", "chore: init")...)

	return Repo{Dir: dir}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	writeFile(t, dir, name, content)
	run(t, dir, "add", name)
	run(t, dir, append(identity, "commit", "-m", message)...)
}

var feature = Feature{Slug: "add-order-api", BaseBranch: "main"}

func TestPrepareCreatesBranchesAndWorktrees(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	ctx := context.Background()

	for range 2 { // preparing twice must be harmless
		if err := r.Prepare(ctx, feature, []string{"specs", "dev"}); err != nil {
			t.Fatal(err)
		}
	}

	for _, team := range []string{"specs", "dev"} {
		if got := run(t, r.TeamDir(feature, team), "branch", "--show-current"); got != feature.TeamBranch(team) {
			t.Fatalf("team %s worktree is on %q", team, got)
		}
	}

	if status := run(t, r.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("the user's checkout must stay clean, git status shows:\n%s", status)
	}

	if branch, err := r.CurrentBranch(ctx); err != nil || branch != "main" {
		t.Fatalf("the user's checkout must stay on main, got %q (%v)", branch, err)
	}
}

func TestRelayBetweenTeams(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	ctx := context.Background()

	if err := r.Prepare(ctx, feature, []string{"specs", "dev"}); err != nil {
		t.Fatal(err)
	}

	// specs commits and delivers (fast-forward of the feature branch).
	commitFile(t, r.TeamDir(feature, "specs"), "SPEC.md", "the spec\n", "docs: write the spec")

	commits, err := r.Deliver(ctx, feature, "specs")
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 1 || commits[0].Subject != "docs: write the spec" {
		t.Fatalf("delivered commits = %+v", commits)
	}

	// dev claims: its worktree receives the spec.
	if err := r.Sync(ctx, feature, "dev"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(r.TeamDir(feature, "dev"), "SPEC.md")); err != nil {
		t.Fatal("dev should have received the spec when claiming")
	}

	// Meanwhile specs delivers again, so dev's delivery needs a real merge.
	commitFile(t, r.TeamDir(feature, "specs"), "SPEC2.md", "more\n", "docs: add details")

	if _, err := r.Deliver(ctx, feature, "specs"); err != nil {
		t.Fatal(err)
	}

	commitFile(t, r.TeamDir(feature, "dev"), "main.go", "package main\n", "feat: implement")

	if _, err := r.Deliver(ctx, feature, "dev"); err != nil {
		t.Fatal(err)
	}

	files := run(t, r.Dir, "ls-tree", "--name-only", feature.Branch())
	for _, want := range []string{"SPEC.md", "SPEC2.md", "main.go"} {
		if !strings.Contains(files, want) {
			t.Fatalf("feature branch misses %s:\n%s", want, files)
		}
	}
}

func TestDeliverWithNothingNew(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	if err := r.Prepare(context.Background(), feature, []string{"dev"}); err != nil {
		t.Fatal(err)
	}

	commits, err := r.Deliver(context.Background(), feature, "dev")
	if err != nil || len(commits) != 0 {
		t.Fatalf("nothing to deliver: commits %+v, err %v", commits, err)
	}
}

func TestDeliverConflict(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	ctx := context.Background()

	if err := r.Prepare(ctx, feature, []string{"specs", "dev"}); err != nil {
		t.Fatal(err)
	}

	commitFile(t, r.TeamDir(feature, "specs"), "README.md", "specs version\n", "docs: specs")

	if _, err := r.Deliver(ctx, feature, "specs"); err != nil {
		t.Fatal(err)
	}

	commitFile(t, r.TeamDir(feature, "dev"), "README.md", "dev version\n", "docs: dev")

	before := run(t, r.Dir, "rev-parse", feature.Branch())

	if _, err := r.Deliver(ctx, feature, "dev"); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}

	if after := run(t, r.Dir, "rev-parse", feature.Branch()); after != before {
		t.Fatal("a failed delivery must leave the feature branch unchanged")
	}

	if worktrees := run(t, r.Dir, "worktree", "list"); strings.Contains(worktrees, "/merge/") {
		t.Fatalf("the temporary merge worktree must be removed:\n%s", worktrees)
	}
}

func TestDirty(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	if err := r.Prepare(context.Background(), feature, []string{"dev"}); err != nil {
		t.Fatal(err)
	}

	dirty, err := r.Dirty(context.Background(), feature, "dev")
	if err != nil || len(dirty) != 0 {
		t.Fatalf("fresh worktree: dirty %v, err %v", dirty, err)
	}

	writeFile(t, r.TeamDir(feature, "dev"), "wip.go", "package wip\n")

	dirty, err = r.Dirty(context.Background(), feature, "dev")
	if err != nil || len(dirty) != 1 || !strings.Contains(dirty[0], "wip.go") {
		t.Fatalf("dirty = %v, err %v", dirty, err)
	}
}

func TestPrepareOutsideARepository(t *testing.T) {
	t.Parallel()

	err := Repo{Dir: t.TempDir()}.Prepare(context.Background(), feature, []string{"dev"})
	if !errors.Is(err, ErrNotARepository) {
		t.Fatalf("error = %v, want ErrNotARepository", err)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Add the order API":    "add-the-order-api",
		"  Fix: login (v2)!  ": "fix-login-v2",
		"???":                  "feature",
		"A very long feature title that goes on and on forever": "a-very-long-feature-title-that-goes-on-a",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
