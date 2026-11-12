package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
	"github.com/qwackididuck/nobodyinc/internal/workspace"
)

// gitWorkspace connects tickets to the project's git repository: it is the
// work.Workspace the ticket service calls on claims and closings, and it
// knows which directory each agent works in (see docs/workspaces.md).
type gitWorkspace struct {
	repo workspace.Repo
	org  org.Source

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

// BeforeDone checks the team's worktree is committed, then delivers the
// team's branch to the feature branch. The commits become deliverables.
func (g *gitWorkspace) BeforeDone(ctx context.Context, f work.Feature, agent string) ([]work.Deliverable, error) {
	wf, team := toWorkspace(f), org.TeamOf(agent)

	dirty, err := g.repo.Dirty(ctx, wf, team)
	if err != nil {
		return nil, err
	}

	if len(dirty) > 0 {
		return nil, fmt.Errorf("%w:\n%s", workspace.ErrDirty, strings.Join(dirty, "\n"))
	}

	commits, err := g.repo.Deliver(ctx, wf, team)
	if err != nil {
		return nil, err
	}

	deliverables := make([]work.Deliverable, 0, len(commits))
	for _, c := range commits {
		deliverables = append(deliverables, work.Deliverable{
			Author: agent, Kind: "commit", Reference: c.Hash, Summary: c.Subject,
		})
	}

	return deliverables, nil
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

	if g.active == nil {
		return g.repo.Dir
	}

	return g.repo.TeamDir(toWorkspace(*g.active), org.TeamOf(address))
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
