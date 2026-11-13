// Package staffing recruits, edits, puts on leave and dismisses agents while
// the daemon runs (docs/agents.md, "Lifecycle").
//
// Every change follows the same steps, one change at a time:
//
//  1. build the organization as it would be after the change, from the
//     current snapshot, and validate it (unknown team, MCP server, profile...),
//     so that nothing invalid is ever written;
//  2. write the agent's file in the project's .nobody/ (or delete it);
//  3. reload the organization from disk (org.Live);
//  4. ask the supervisor to apply it: the agent starts, restarts or stops
//     once its current turn is over (runtime.Supervisor.Apply).
//
// The files stay the source of truth: this package only writes what a human
// could write by hand.
package staffing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Errors returned by the service; the API maps them to HTTP statuses.
var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
)

// Applier brings a running agent in line with its definition (implemented by
// runtime.Supervisor).
type Applier interface {
	Apply(ctx context.Context, address string, opts runtime.ApplyOptions) error
}

// Service changes the organization's agents.
type Service struct {
	Org     *org.Live
	Catalog *catalog.Catalog
	Agents  Applier

	mu sync.Mutex // one change at a time: each one starts from the latest snapshot
}

// Recruit copies a template into a team under a name, then starts the agent.
//
// ctx bounds the agent's process: it must outlive the caller's request.
func (s *Service) Recruit(ctx context.Context, team, name, template string) (org.Member, error) {
	t, err := s.Catalog.Get(template)
	if errors.Is(err, catalog.ErrNotFound) {
		return org.Member{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}

	if err != nil {
		return org.Member{}, err
	}

	return s.hire(ctx, team, name, t.Recruit())
}

// Create adds an agent written from scratch to a team, then starts it.
func (s *Service) Create(ctx context.Context, team, name string, d catalog.Definition) (org.Member, error) {
	d.From = ""

	return s.hire(ctx, team, name, d)
}

// Definition returns an agent's definition, as in its file.
func (s *Service) Definition(address string) (catalog.Definition, error) {
	m, err := s.member(address)

	return m.Agent.Definition, err
}

// Edit replaces an agent's definition and restarts it with it. Where it came
// from and whether it is on leave are kept: they change through Recruit and
// Leave/Return. The template it came from is never touched.
func (s *Service) Edit(ctx context.Context, address string, d catalog.Definition, opts runtime.ApplyOptions) (catalog.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.member(address)
	if err != nil {
		return catalog.Definition{}, err
	}

	d = agentFields(d)
	d.From = m.Agent.From
	d.OnLeave = m.Agent.OnLeave

	if err := s.save(ctx, m.Team.ID, org.Agent{Name: m.Agent.Name, Definition: d}, opts); err != nil {
		return catalog.Definition{}, err
	}

	return d, nil
}

// Leave puts an agent on leave: it stops once idle (or at once with
// opts.Now), keeping its file and session. Putting on leave an agent already
// on leave does nothing.
func (s *Service) Leave(ctx context.Context, address string, opts runtime.ApplyOptions) error {
	return s.setLeave(ctx, address, true, opts)
}

// Return brings an agent back from leave: it starts again, resuming its
// session unless opts.Fresh.
func (s *Service) Return(ctx context.Context, address string, opts runtime.ApplyOptions) error {
	return s.setLeave(ctx, address, false, opts)
}

// Dismiss deletes an agent's file; it stops once idle (or at once with
// opts.Now) and hands its ticket and mail over (see daemon/staff.go).
func (s *Service) Dismiss(ctx context.Context, address string, opts runtime.ApplyOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.member(address)
	if err != nil {
		return err
	}

	o := s.Org.Current()
	if err := org.RemoveAgent(o.ProjectDir, m.Team.ID, m.Agent.Name); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, address, opts)
}

func (s *Service) setLeave(ctx context.Context, address string, onLeave bool, opts runtime.ApplyOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.member(address)
	if err != nil {
		return err
	}

	if m.Agent.OnLeave == onLeave {
		return nil
	}

	a := m.Agent
	a.OnLeave = onLeave

	return s.save(ctx, m.Team.ID, a, opts)
}

// hire adds a new agent and starts it with a fresh session: an address may
// have belonged to a dismissed agent, whose session must not be resumed.
func (s *Service) hire(ctx context.Context, team, name string, d catalog.Definition) (org.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	address := org.Address(name, team)

	if !catalog.ValidName(name) {
		return org.Member{}, fmt.Errorf("%w: agent name %q: use lowercase letters, digits and dashes", ErrInvalid, name)
	}

	if _, exists := s.Org.Current().Member(address); exists {
		return org.Member{}, fmt.Errorf("%w: agent %s", ErrExists, address)
	}

	d = agentFields(d)
	d.OnLeave = false

	if err := s.save(ctx, team, org.Agent{Name: name, Definition: d}, runtime.ApplyOptions{Fresh: true}); err != nil {
		return org.Member{}, err
	}

	m, _ := s.Org.Current().Member(address)

	return m, nil
}

// save validates the organization with agent a in team, writes a's file,
// reloads and applies. Called with s.mu held.
func (s *Service) save(ctx context.Context, team string, a org.Agent, opts runtime.ApplyOptions) error {
	current := s.Org.Current()

	candidate, err := current.WithAgent(team, a)
	if errors.Is(err, org.ErrUnknownTeam) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}

	if err != nil {
		return err
	}

	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if err := org.WriteAgent(current.ProjectDir, team, a); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, org.Address(a.Name, team), opts)
}

func (s *Service) reloadAndApply(ctx context.Context, address string, opts runtime.ApplyOptions) error {
	// A reload can only fail here if another file was broken by hand meanwhile.
	if _, err := s.Org.Reload(); err != nil {
		return fmt.Errorf("saved, but the organization cannot be reloaded (fix .nobody/ by hand): %w", err)
	}

	return s.Agents.Apply(ctx, address, opts)
}

func (s *Service) member(address string) (org.Member, error) {
	m, ok := s.Org.Current().Member(address)
	if !ok {
		return org.Member{}, fmt.Errorf("%w: agent %s", ErrNotFound, address)
	}

	return m, nil
}

// agentFields drops the fields that only make sense in a template.
func agentFields(d catalog.Definition) catalog.Definition {
	d.Description = ""
	d.Version = 0

	return d
}
