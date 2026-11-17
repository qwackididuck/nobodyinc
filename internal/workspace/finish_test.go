package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// deliveredRepo is a repository where team dev delivered orders.go to the
// feature branch.
func deliveredRepo(t *testing.T) Repo {
	t.Helper()

	r := newRepo(t)
	ctx := context.Background()

	if err := r.Prepare(ctx, feature, []string{"dev"}); err != nil {
		t.Fatal(err)
	}

	commitFile(t, r.TeamDir(feature, "dev"), "orders.go", "package orders\n", "feat: add orders")

	if _, err := r.Deliver(ctx, feature, "dev"); err != nil {
		t.Fatal(err)
	}

	return r
}

func TestReview(t *testing.T) {
	t.Parallel()

	r := deliveredRepo(t)
	ctx := context.Background()

	review, err := r.Review(ctx, feature, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	if len(review.Commits) != 1 || review.Commits[0].Subject != "feat: add orders" || review.Commits[0].Author != "Test" {
		t.Fatalf("commits = %+v", review.Commits)
	}

	if len(review.Files) != 1 || review.Files[0] != (FileChange{Path: "orders.go", Status: "A", Added: 1, Deleted: 0}) {
		t.Fatalf("files = %+v", review.Files)
	}

	if !strings.Contains(review.Diff, "+package orders") || review.Truncated {
		t.Fatalf("diff = %q (truncated %v)", review.Diff, review.Truncated)
	}

	short, err := r.Review(ctx, feature, 10)
	if err != nil || len(short.Diff) != 10 || !short.Truncated {
		t.Fatalf("a diff over the limit must be cut: %q, %v, %v", short.Diff, short.Truncated, err)
	}
}

func TestFinish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// arrange changes the project before finishing.
		arrange func(t *testing.T, r Repo)
		wantErr error
		// merged tells whether main must contain the feature afterwards.
		merged bool
	}{
		{"base checked out and clean: merged in the project", func(*testing.T, Repo) {}, nil, true},
		{
			"base checked out with uncommitted changes: refused",
			func(t *testing.T, r Repo) {
				t.Helper()
				writeFile(t, r.Dir, "README.md", "work in progress\n")
			},
			ErrBaseDirty, false,
		},
		{
			"untracked files do not block",
			func(t *testing.T, r Repo) {
				t.Helper()
				writeFile(t, r.Dir, "notes.txt", "mine\n")
			},
			nil, true,
		},
		{
			"base not checked out: merged in a temporary worktree",
			func(t *testing.T, r Repo) {
				t.Helper()
				run(t, r.Dir, "switch", "-c", "elsewhere")
			},
			nil, true,
		},
		{
			"conflict: nothing changes",
			func(t *testing.T, r Repo) {
				t.Helper()
				commitFile(t, r.Dir, "orders.go", "package other\n", "feat: another orders")
			},
			ErrBaseConflict, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := deliveredRepo(t)
			tt.arrange(t, r)
			before := run(t, r.Dir, "rev-parse", "main")

			commit, err := r.Finish(context.Background(), feature, "Merge feature F-1: Add order API")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			after := run(t, r.Dir, "rev-parse", "main")
			if !tt.merged {
				if after != before {
					t.Fatal("main moved although the finish failed")
				}

				if strings.Contains(run(t, r.Dir, "status", "--porcelain"), "UU") {
					t.Fatal("a failed merge must be aborted")
				}

				return
			}

			if commit != after {
				t.Fatalf("returned commit %s, main is at %s", commit, after)
			}

			if parents := strings.Fields(run(t, r.Dir, "log", "-1", "--format=%P", "main")); len(parents) != 2 {
				t.Fatalf("finishing must create a merge commit, parents = %v", parents)
			}

			if author := run(t, r.Dir, "log", "-1", "--format=%an", "main"); author != "Nobody Inc" {
				t.Fatalf("merge signed by %q", author)
			}

			run(t, r.Dir, "cat-file", "-e", "main:orders.go")
		})
	}
}

func TestFinishWithNothingNew(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	if err := r.Prepare(context.Background(), feature, []string{"dev"}); err != nil {
		t.Fatal(err)
	}

	commit, err := r.Finish(context.Background(), feature, "Merge")
	if err != nil || commit != "" {
		t.Fatalf("nothing to merge: commit %q, error %v", commit, err)
	}
}

func TestCleanup(t *testing.T) {
	t.Parallel()

	r := deliveredRepo(t)
	ctx := context.Background()

	if err := r.Cleanup(ctx, feature, []string{"dev", "specs"}); err != nil {
		t.Fatal(err)
	}

	if dirExists(r.TeamDir(feature, "dev")) {
		t.Fatal("the team's worktree is still there")
	}

	if r.branchExists(ctx, feature.TeamBranch("dev")) {
		t.Fatal("the team's branch is still there")
	}

	if !r.branchExists(ctx, feature.Branch()) {
		t.Fatal("the feature branch must be kept")
	}
}

// A team deleted while the feature was active is no longer in the list:
// its worktree and branch go anyway.
func TestCleanupRemovesDeletedTeams(t *testing.T) {
	t.Parallel()

	r := deliveredRepo(t)
	ctx := context.Background()

	if err := r.Cleanup(ctx, feature, nil); err != nil {
		t.Fatal(err)
	}

	if dirExists(r.TeamDir(feature, "dev")) || r.branchExists(ctx, feature.TeamBranch("dev")) {
		t.Fatal("the deleted team's worktree or branch is still there")
	}
}

func TestRenamedPath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"plain.go":                 "plain.go",
		"old.go => new.go":         "new.go",
		"dir/{old => new}/file.go": "dir/new/file.go",
		"dir/{a.go => b.go}":       "dir/b.go",
	}
	for in, want := range tests {
		if got := renamedPath(in); got != want {
			t.Errorf("renamedPath(%q) = %q, want %q", in, got, want)
		}
	}
}
