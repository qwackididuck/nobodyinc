package staffing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Teams and the project prompt (docs/agents.md, "The prompt cascade").
//
// A team is declared in org.yaml (id, goal, block list); its shared
// instructions are in teams/<team>/TEAM.md, and the project's in project.md.
// Both are part of the system prompt of the agents they concern: when they
// change, those agents restart with a fresh session once idle, as for new
// instructions. The block list is not (ADR-0009): when it changes, the agents
// concerned get a notice instead (daemon/orgnotice.go) and go on.

// CreateTeam declares a new team, without agents yet: org.yaml gets its id,
// goal and block list, TEAM.md its prompt (none when empty).
func (s *Service) CreateTeam(ctx context.Context, t org.Team) (org.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !catalog.ValidName(t.ID) {
		return org.Team{}, fmt.Errorf("%w: team id %q: use lowercase letters, digits and dashes", ErrInvalid, t.ID)
	}

	if _, exists := s.Org.Current().Team(t.ID); exists {
		return org.Team{}, fmt.Errorf("%w: team %q", ErrExists, t.ID)
	}

	return s.saveTeam(ctx, cleanTeam(t))
}

// UpdateTeam replaces a team's goal, prompt and block list. Its members
// restart with a fresh session once idle if the goal or prompt changed.
func (s *Service) UpdateTeam(ctx context.Context, t org.Team) (org.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Org.Current().Team(t.ID); !exists {
		return org.Team{}, fmt.Errorf("%w: team %q", ErrNotFound, t.ID)
	}

	return s.saveTeam(ctx, cleanTeam(t))
}

// SetBlock replaces a team's block list alone: the teams its members may not
// start a conversation with. Nobody restarts.
func (s *Service) SetBlock(ctx context.Context, id string, block []string) (org.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, exists := s.Org.Current().Team(id)
	if !exists {
		return org.Team{}, fmt.Errorf("%w: team %q", ErrNotFound, id)
	}

	t.Block = block

	return s.saveTeam(ctx, cleanTeam(t))
}

// cleanTeam trims what a person typed; an empty block list is nil, so that
// org.yaml does not show "block: []".
func cleanTeam(t org.Team) org.Team {
	t.Goal = strings.TrimSpace(t.Goal)
	t.Prompt = strings.TrimSpace(t.Prompt)

	var block []string

	for _, b := range t.Block {
		if b = strings.TrimSpace(b); b != "" {
			block = append(block, b)
		}
	}

	t.Block = block

	return t
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

// SetKnowledge sets the project's knowledge base up at path, moves it, or
// removes it when path is "" (ADR-0019): "knowledge: {path}" in org.yaml.
// The path is checked with its symbolic links followed (kb.Resolve), and a
// folder that does not exist is created, empty; removing the knowledge base
// never touches the files. Every agent restarts once idle, resuming its
// session: the knowledge tools of the office MCP come and go with it.
func (s *Service) SetKnowledge(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Org.Current()

	if path != "" {
		clean, err := kb.CheckPath(path)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		dir, err := kb.Resolve(current.ProjectDir, clean)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		if err := os.MkdirAll(dir, kb.FolderMode); err != nil {
			return fmt.Errorf("create the knowledge folder %s: %w", clean, err)
		}

		path = clean
	}

	if current.KnowledgePath() == path {
		return nil
	}

	candidate := current.WithKnowledge(path)
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if err := org.WriteOrgFile(candidate); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{})
}

// DeleteTeam removes a team and dismisses its members: its directory (agent
// files and TEAM.md) is deleted first, then org.yaml is written without the
// team, which also leaves the other teams' block lists. Each member stops once idle (or at once with
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
