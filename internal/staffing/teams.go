package staffing

import (
	"context"
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Teams and the project prompt (docs/agents.md, "The prompt cascade").
//
// A team is declared in org.yaml (id, goal); its shared instructions are in
// teams/<team>/TEAM.md, and the project's in project.md. Both are part of the
// system prompt of the agents they concern: when they change, those agents
// restart with a fresh session once idle, as for new instructions.

// CreateTeam declares a new team, without agents yet: org.yaml gets its id
// and goal, TEAM.md its prompt (none when empty).
func (s *Service) CreateTeam(ctx context.Context, id, goal, prompt string) (org.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !catalog.ValidName(id) {
		return org.Team{}, fmt.Errorf("%w: team id %q: use lowercase letters, digits and dashes", ErrInvalid, id)
	}

	if _, exists := s.Org.Current().Team(id); exists {
		return org.Team{}, fmt.Errorf("%w: team %q", ErrExists, id)
	}

	return s.saveTeam(ctx, org.Team{ID: id, Goal: strings.TrimSpace(goal), Prompt: strings.TrimSpace(prompt)})
}

// UpdateTeam replaces a team's goal and prompt. Its members restart with a
// fresh session once idle.
func (s *Service) UpdateTeam(ctx context.Context, id, goal, prompt string) (org.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Org.Current().Team(id); !exists {
		return org.Team{}, fmt.Errorf("%w: team %q", ErrNotFound, id)
	}

	return s.saveTeam(ctx, org.Team{ID: id, Goal: strings.TrimSpace(goal), Prompt: strings.TrimSpace(prompt)})
}

// SetProjectPrompt replaces the project prompt (.nobody/project.md; empty
// removes it). Every agent restarts with a fresh session once idle.
func (s *Service) SetProjectPrompt(ctx context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := org.WriteProjectPrompt(s.Org.Current().ProjectDir, text); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{})
}

// DeleteTeam removes a team and dismisses its members: its directory (agent
// files and TEAM.md) is deleted first, then org.yaml is written without the
// team and its channels. Each member stops once idle (or at once with
// opts.Now) and hands its ticket and mail over, as a dismissed agent does
// (daemon/staff.go). The team's tickets are left as they are.
func (s *Service) DeleteTeam(ctx context.Context, id string, opts runtime.ApplyOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Org.Current()
	if _, exists := current.Team(id); !exists {
		return fmt.Errorf("%w: team %q", ErrNotFound, id)
	}

	candidate := current.WithoutTeam(id)
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if err := org.RemoveTeamDir(current.ProjectDir, id); err != nil {
		return err
	}

	if err := org.WriteOrgFile(candidate); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{Now: opts.Now})
}

// saveTeam validates the organization with team t, writes org.yaml then
// TEAM.md (in this order: a team directory org.yaml does not declare would
// stop the organization from loading), reloads and applies. Called with s.mu
// held.
func (s *Service) saveTeam(ctx context.Context, t org.Team) (org.Team, error) {
	current := s.Org.Current()

	candidate := current.WithTeam(t)
	if err := candidate.Validate(); err != nil {
		return org.Team{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if err := org.WriteOrgFile(candidate); err != nil {
		return org.Team{}, err
	}

	if err := org.WriteTeamPrompt(current.ProjectDir, t.ID, t.Prompt); err != nil {
		return org.Team{}, err
	}

	if err := s.reloadAndApply(ctx, "", runtime.ApplyOptions{}); err != nil {
		return org.Team{}, err
	}

	saved, _ := s.Org.Current().Team(t.ID)

	return saved, nil
}
