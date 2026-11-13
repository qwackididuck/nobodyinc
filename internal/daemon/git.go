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
	// project's .nobody/ (settings.ProtectBlock or ProtectWarn).
	configProtection string
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

// checkConfigChanges looks for changes to the project's .nobody/ in the
// team's work (ADR-0004). In block mode they refuse the delivery, with the
// way to undo them; in warn mode they are returned, to warn the human once
// delivered.
func (g *gitWorkspace) checkConfigChanges(ctx context.Context, f workspace.Feature, team string) ([]string, error) {
	files, err := g.repo.ConfigChanges(ctx, f, team)
	if err != nil || len(files) == 0 {
		return nil, err
	}

	if g.configProtection == settings.ProtectWarn {
		return files, nil
	}

	return nil, fmt.Errorf("%w:\n%s\nPut %s back as it is on %s, commit, then close the ticket again:\n"+
		"  git restore --source=%s --staged --worktree -- %s   (removes the files you added there too)\n"+
		"  git commit -m \"revert: leave .nobody alone\"",
		workspace.ErrConfigChanged, strings.Join(files, "\n"), workspace.ConfigDir, f.Branch(), f.Branch(), workspace.ConfigDir)
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
