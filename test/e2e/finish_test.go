package e2e

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Finishing a feature (ADR-0007): once its ticket is done, the human reviews
// it and finishes it. The feature branch is merged into main with a merge
// commit, the feature is no longer active, the team agents restart in the
// project, and the teams' worktrees are removed.
func TestFinishFeature(t *testing.T) {
	t.Parallel()

	o := testOrg(t)
	initGitProject(t, o.ProjectDir)

	d := startDaemon(t, o)
	ctx := context.Background()

	before := outputs(d, o)

	f, err := d.Tickets.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	waitRestarted(t, d, o, before)

	worktree := d.WorkDir("dev1@dev")

	impl, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Implement", Team: "dev"})
	if err != nil {
		t.Fatal(err)
	}

	waitForDone(t, d, impl.ID)

	review, err := d.Tickets.ReviewFeature(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(review.Files) != 1 || review.Files[0].Path != impl.ID+".md" || len(review.UnfinishedTickets) != 0 {
		t.Fatalf("review = files %+v, unfinished %+v", review.Files, review.UnfinishedTickets)
	}

	before = outputs(d, o)

	finished, err := d.Tickets.FinishFeature(ctx, org.HumanAddress, f.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	if finished.Status != work.FeatureFinished || finished.Active || finished.MergeCommit == "" {
		t.Fatalf("finished feature = %+v", finished)
	}

	if files := git(t, o.ProjectDir, "ls-tree", "--name-only", "main"); !strings.Contains(files, impl.ID+".md") {
		t.Fatalf("main should hold the feature's work:\n%s", files)
	}

	if subject := git(t, o.ProjectDir, "log", "-1", "--format=%s", "main"); subject != "Merge feature "+f.ID+": Add the order API" {
		t.Fatalf("merge commit subject = %q", subject)
	}

	waitRestarted(t, d, o, before)

	if dir := d.WorkDir("dev1@dev"); dir != o.ProjectDir {
		t.Fatalf("dev1 should work in the project again, works in %s", dir)
	}

	eventually(t, "the teams' worktrees removed", func() bool {
		_, err := os.Stat(worktree)

		return os.IsNotExist(err)
	})

	if branches := git(t, o.ProjectDir, "branch", "--list", "nobody/*"); strings.Contains(branches, "--dev") || !strings.Contains(branches, f.Branch) {
		t.Fatalf("team branches should be gone and the feature branch kept:\n%s", branches)
	}
}
