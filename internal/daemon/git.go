package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/work"
	"github.com/qwackididuck/nobodyinc/internal/workspace"
)

// gitWorkspace connects tickets to the project's git repository: it is the
// work.Workspace the ticket service calls on claims and closings, and it
// knows which directory each agent works in (see docs/workspaces.md).
type gitWorkspace struct {
	repo workspace.Repo
	org  org.Source
	// configProtection says what to do with a delivery that changes the
	// project's .nobody/ (settings.ProtectBlock or ProtectWarn). It is read on
	// every delivery: the control room can change it while the daemon runs.
	configProtection func() string
	// onConfigDelivered is told about such a delivery in warn mode.
	onConfigDelivered func(agent string, f work.Feature, files []string)

	mu     sync.Mutex
	active *work.Feature // nil until a feature is started
}

// PrepareFeature fills in the feature's slug and branches, then creates the
// feature branch and one worktree per team.
func (g *gitWorkspace) PrepareFeature(ctx context.Context, f work.Feature) (work.Feature, error) {
	if f.Slug == "" {
		f.Slug = workspace.Slugify(f.Title)
	}

	if f.BaseBranch == "" {
		base, err := g.repo.CurrentBranch(ctx)
		if err != nil {
			return work.Feature{}, fmt.Errorf("%w: %w", workspace.ErrNotARepository, err)
		}

		f.BaseBranch = base
	}

	wf := toWorkspace(f)
	f.Branch = wf.Branch()

	if err := g.repo.Prepare(ctx, wf, g.teams()); err != nil {
		return work.Feature{}, err
	}

	return f, nil
}

// BeforeClaim brings the feature branch into the claimer's team branch.
func (g *gitWorkspace) BeforeClaim(ctx context.Context, f work.Feature, agent string) error {
	return g.repo.Sync(ctx, toWorkspace(f), org.TeamOf(agent))
}

// BeforeDone checks the team's worktree is committed and that the work does
// not change the agents' configuration, then delivers the team's branch to
// the feature branch. The commits become deliverables.
func (g *gitWorkspace) BeforeDone(ctx context.Context, f work.Feature, agent string) ([]work.Deliverable, error) {
	wf, team := toWorkspace(f), org.TeamOf(agent)

	dirty, err := g.repo.Dirty(ctx, wf, team)
	if err != nil {
		return nil, err
	}

	if len(dirty) > 0 {
		return nil, fmt.Errorf("%w:\n%s", workspace.ErrDirty, strings.Join(dirty, "\n"))
	}

	configFiles, err := g.checkConfigChanges(ctx, wf, team)
	if err != nil {
		return nil, err
	}

	commits, err := g.repo.Deliver(ctx, wf, team)
	if err != nil {
		return nil, err
	}

	if len(configFiles) > 0 && g.onConfigDelivered != nil {
		g.onConfigDelivered(agent, f, configFiles)
	}

	deliverables := make([]work.Deliverable, 0, len(commits))
	for _, c := range commits {
		deliverables = append(deliverables, work.Deliverable{
			Author: agent, Kind: "commit", Reference: c.Hash, Summary: c.Subject,
		})
	}

	return deliverables, nil
}

// maxReviewDiff is where the diff of a feature review is cut.
const maxReviewDiff = 400 << 10

// ReviewFeature describes what the feature branch brings to its base.
func (g *gitWorkspace) ReviewFeature(ctx context.Context, f work.Feature) (work.Changes, error) {
	r, err := g.repo.Review(ctx, toWorkspace(f), maxReviewDiff)
	if err != nil {
		return work.Changes{}, err
	}

	changes := work.Changes{Diff: r.Diff, DiffTruncated: r.Truncated, Commits: []work.ReviewCommit{}, Files: []work.FileChange{}}
	for _, c := range r.Commits {
		changes.Commits = append(changes.Commits, work.ReviewCommit(c))
	}

	for _, fc := range r.Files {
		changes.Files = append(changes.Files, work.FileChange(fc))
	}

	return changes, nil
}

// FinishFeature merges the feature branch into its base branch (ADR-0007).
// The teams' worktrees are removed later, once the agents left them
// (cleanup).
func (g *gitWorkspace) FinishFeature(ctx context.Context, f work.Feature) (string, error) {
	return g.repo.Finish(ctx, toWorkspace(f), fmt.Sprintf("Merge feature %s: %s", f.ID, f.Title))
}

// checkConfigChanges looks for changes to the project's .nobody/ in the
// team's work (ADR-0004). In block mode they refuse the delivery, with the
// way to undo them; in warn mode they are returned, to warn the human once
// delivered.
func (g *gitWorkspace) checkConfigChanges(ctx context.Context, f workspace.Feature, team string) ([]string, error) {
	files, err := g.repo.ConfigChanges(ctx, f, team)
	if err != nil || len(files) == 0 {
		return nil, err
	}

	if g.configProtection != nil && g.configProtection() == settings.ProtectWarn {
		return files, nil
	}

	return nil, fmt.Errorf("%w:\n%s\nPut %s back as it is on %s, commit, then close the ticket again:\n"+
		"  git restore --source=%s --staged --worktree -- %s   (removes the files you added there too)\n"+
		"  git commit -m \"revert: leave .nobody alone\"",
		workspace.ErrConfigChanged, strings.Join(files, "\n"), workspace.ConfigDir, f.Branch(), f.Branch(), workspace.ConfigDir)
}

// deactivate sends agents back to the project directory: no feature is
// active any more.
func (g *gitWorkspace) deactivate() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active = nil
}

// cleanup removes the teams' worktrees and branches of a finished feature.
func (g *gitWorkspace) cleanup(ctx context.Context, f work.Feature) error {
	return g.repo.Cleanup(ctx, toWorkspace(f), g.teams())
}

// prepareNewTeams gives the teams created while a feature is active their
// worktree and branch, before their first agent starts in it.
func (g *gitWorkspace) prepareNewTeams(ctx context.Context, before, after *org.Org) error {
	g.mu.Lock()
	active := g.active
	g.mu.Unlock()

	if active == nil {
		return nil
	}

	var added []string

	for _, t := range after.Teams {
		if _, existed := before.Team(t.ID); !existed {
			added = append(added, t.ID)
		}
	}

	if len(added) == 0 {
		return nil
	}

	return g.repo.Prepare(ctx, toWorkspace(*active), added)
}

// activate records the feature agents now work on.
func (g *gitWorkspace) activate(f work.Feature) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active = &f
}

// workDir is where an agent works: its team's worktree for the active
// feature, or the project itself when no feature is active.
func (g *gitWorkspace) workDir(address string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Without a feature, everyone works in the project. The director (no
	// team) always does: it reads and plans, it does not deliver code.
	team := org.TeamOf(address)
	if g.active == nil || team == "" {
		return g.repo.Dir
	}

	return g.repo.TeamDir(toWorkspace(*g.active), team)
}

func (g *gitWorkspace) teams() []string {
	o := g.org.Current()

	teams := make([]string, 0, len(o.Teams))
	for _, t := range o.Teams {
		teams = append(teams, t.ID)
	}

	return teams
}

func toWorkspace(f work.Feature) workspace.Feature {
	return workspace.Feature{Slug: f.Slug, BaseBranch: f.BaseBranch}
}
