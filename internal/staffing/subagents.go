package staffing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/subagents"
)

// Importing the project's subagents (ADR-0018): the human creates teams and
// puts the project's Claude Code subagents in them, as agents, and dismisses
// the others, in one go. Everything is checked against the organization
// with all of it before anything is written.

// SubagentImport is what the human chose in the import window.
type SubagentImport struct {
	// Teams are the new teams (id and goal).
	Teams []org.Team
	// Agents are the subagents to import.
	Agents []ImportedAgent
	// Ignore are the names of the subagents to dismiss.
	Ignore []string
}

// ImportedAgent is one subagent to import into a team, under a name.
type ImportedAgent struct {
	// Subagent is the subagent's name.
	Subagent string
	Team     string
	// Name is the agent's name in its team.
	Name string
	// Profile replaces the permission profile converted from the subagent;
	// "" keeps it.
	Profile string
}

// ImportSubagents creates the new teams, the agents imported from the
// subagents and the list of dismissed subagents, then starts the new agents
// with a fresh session, as recruits. It returns their addresses. Nothing is
// written when one choice is refused: the error lists every refusal.
//
// ctx bounds the agents' processes: it must outlive the caller's request.
func (s *Service) ImportSubagents(ctx context.Context, imp SubagentImport) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Org.Current()

	all, err := subagents.Load(current.ProjectDir)
	if err != nil {
		return nil, err
	}

	plan := importPlan{service: s, current: current, candidate: current, pending: map[string]subagents.Subagent{}}
	for _, sub := range subagents.Pending(current, all) {
		plan.pending[sub.Name] = sub
	}

	plan.addTeams(imp.Teams)
	plan.addAgents(imp.Agents)
	plan.ignore(imp.Ignore)

	if len(imp.Teams)+len(imp.Agents)+len(imp.Ignore) == 0 {
		plan.refusef("nothing to import or dismiss")
	}

	if len(plan.refusals) == 0 {
		if err := plan.candidate.Validate(); err != nil {
			plan.refusals = append(plan.refusals, err)
		}
	}

	if len(plan.refusals) > 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, errors.Join(plan.refusals...))
	}

	if err := plan.write(); err != nil {
		return nil, err
	}

	return plan.addresses, s.reloadAndStart(ctx, current, plan.addresses)
}

// importPlan checks the human's choices one by one, building the
// organization with all of them.
type importPlan struct {
	service *Service
	current *org.Org
	// candidate is the organization with every choice accepted so far.
	candidate *org.Org
	// pending are the subagents that may be imported or dismissed, by name.
	pending map[string]subagents.Subagent
	// used are the subagents already imported or dismissed by this import.
	used      []string
	agents    []org.Member
	addresses []string
	refusals  []error
}

func (p *importPlan) refusef(format string, args ...any) {
	p.refusals = append(p.refusals, fmt.Errorf(format, args...))
}

// addTeams accepts the new teams that are valid and free.
func (p *importPlan) addTeams(teams []org.Team) {
	for _, t := range teams {
		t = cleanTeam(t)

		switch {
		case !catalog.ValidName(t.ID):
			p.refusef("team id %q: use lowercase letters, digits and dashes", t.ID)
		case t.ID == org.DirectorTeam:
			p.refusef("team id %q is the director's", t.ID)
		default:
			if _, exists := p.candidate.Team(t.ID); exists {
				p.refusef("team %q already exists", t.ID)

				continue
			}

			p.candidate = p.candidate.WithTeam(org.Team{ID: t.ID, Goal: t.Goal})
		}
	}
}

// addAgents accepts the agents whose subagent is pending, whose team exists
// (or is new) and whose name is valid and free.
func (p *importPlan) addAgents(agents []ImportedAgent) {
	for _, a := range agents {
		sub, ok := p.take(a.Subagent)
		if !ok {
			continue
		}

		if err := p.addAgent(sub, a); err != nil {
			p.refusals = append(p.refusals, fmt.Errorf("subagent %s: %w", a.Subagent, err))
		}
	}
}

func (p *importPlan) addAgent(sub subagents.Subagent, a ImportedAgent) error {
	address := org.Address(a.Name, a.Team)

	switch {
	case a.Team == org.DirectorTeam:
		return errors.New("the director's team takes no agent")
	case !catalog.ValidName(a.Name):
		return fmt.Errorf("agent name %q: use lowercase letters, digits and dashes, starting with a letter", a.Name)
	case org.AgentFileExists(p.current.ProjectDir, a.Team, a.Name):
		return fmt.Errorf("agent %s already exists", address)
	case p.service.Agents.Pending(address):
		return fmt.Errorf("%s is still finishing its turn after a change; try again once it has stopped", address)
	}

	if _, exists := p.candidate.Member(address); exists {
		return fmt.Errorf("agent %s already exists", address)
	}

	d := agentFields(sub.Agent)
	d.OnLeave = false

	if profile := strings.TrimSpace(a.Profile); profile != "" {
		d.Permissions.Profile = profile
	}

	if err := d.Validate(); err != nil {
		return err
	}

	candidate, err := p.candidate.WithAgent(a.Team, org.Agent{Name: a.Name, Definition: d})
	if err != nil {
		return err
	}

	p.candidate = candidate
	m, _ := candidate.Member(address)
	p.agents = append(p.agents, m)
	p.addresses = append(p.addresses, address)

	return nil
}

// ignore accepts the pending subagents to dismiss.
func (p *importPlan) ignore(names []string) {
	var accepted []string

	for _, name := range names {
		if _, ok := p.take(name); ok {
			accepted = append(accepted, name)
		}
	}

	p.candidate = p.candidate.WithIgnoredSubagents(accepted...)
}

// take returns the pending subagent name, once per import.
func (p *importPlan) take(name string) (subagents.Subagent, bool) {
	sub, ok := p.pending[name]

	switch {
	case !ok:
		p.refusef("subagent %q: not found in .claude/agents, already imported or dismissed", name)
	case slices.Contains(p.used, name):
		p.refusef("subagent %q: chosen twice", name)
	default:
		p.used = append(p.used, name)

		return sub, true
	}

	return subagents.Subagent{}, false
}

// write writes org.yaml (new teams, dismissed subagents), then the agent
// files: in this order, as a team directory org.yaml does not declare would
// stop the organization from loading.
func (p *importPlan) write() error {
	if err := org.WriteOrgFile(p.candidate); err != nil {
		return err
	}

	for _, m := range p.agents {
		if err := org.WriteAgent(p.current.ProjectDir, m.Team.ID, m.Agent); err != nil {
			return err
		}
	}

	return nil
}

// reloadAndStart reloads the organization, starts the new agents with a
// fresh session (an address may have belonged to a dismissed agent, whose
// session must not be resumed), and applies the other changes. Called with
// s.mu held.
func (s *Service) reloadAndStart(ctx context.Context, before *org.Org, addresses []string) error {
	after, err := s.Org.Reload()
	if err != nil {
		return fmt.Errorf("saved, but the organization cannot be reloaded (fix .nobody/ by hand): %w", err)
	}

	if s.OnReload != nil {
		s.OnReload(before, after)
	}

	var errs []error

	for _, address := range addresses {
		if err := s.Agents.Apply(ctx, address, runtime.ApplyOptions{Fresh: true}); err != nil {
			errs = append(errs, fmt.Errorf("saved, but %s cannot be started: %w", address, err))
		}
	}

	errs = append(errs, s.applyOthers(ctx, before, after, false, addresses...))

	return errors.Join(errs...)
}
