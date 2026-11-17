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
//     once its current turn is over (runtime.Supervisor.Apply). Other agents
//     whose file changed meanwhile (edited by hand) are applied too.
//
// The files stay the source of truth: this package only writes what a human
// could write by hand.
package staffing

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	// ErrRefused is a change the organization never accepts, such as
	// dismissing the director, who is always present.
	ErrRefused = errors.New("refused")
)

// Applier brings a running agent in line with its definition, and tells
// whether a change still waits for it (implemented by runtime.Supervisor).
type Applier interface {
	Apply(ctx context.Context, address string, opts runtime.ApplyOptions) error
	Pending(address string) bool
}

// Service changes the organization's agents.
type Service struct {
	Org     *org.Live
	Catalog *catalog.Catalog
	Agents  Applier
	// OnReload, when set, is called each time the organization is reloaded
	// after a change, with the snapshots before and after it, before the
	// agents are brought in line with it.
	OnReload func(before, after *org.Org)

	mu sync.Mutex // one change at a time: each one starts from the latest snapshot
}

// Recruit copies a template into a team under a name, then starts the agent.
// The director is not recruited: the daemon creates it (org.EnsureDirector).
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
//
// A change of role or instructions restarts the agent with a fresh session
// (see needsFreshSession); other changes resume its session.
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
	opts.Fresh = opts.Fresh || needsFreshSession(m.Agent.Definition, d)

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
	if o.IsGod(address) {
		return fmt.Errorf("%w: the director is always present: edit it instead", ErrRefused)
	}

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

	// A director put on leave by hand (in its file) can still come back.
	if onLeave && s.Org.Current().IsGod(address) {
		return fmt.Errorf("%w: the director is always present: edit it instead", ErrRefused)
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

	if team == org.DirectorTeam {
		return org.Member{}, fmt.Errorf("%w: the project already has its director, created by the daemon: edit it instead", ErrRefused)
	}

	if !catalog.ValidName(name) {
		return org.Member{}, fmt.Errorf("%w: agent name %q: use lowercase letters, digits and dashes", ErrInvalid, name)
	}

	if _, exists := s.Org.Current().Member(address); exists || org.AgentFileExists(s.Org.Current().ProjectDir, team, name) {
		return org.Member{}, fmt.Errorf("%w: agent %s", ErrExists, address)
	}

	// A dismissed agent may still be finishing its turn: its stop, and the
	// hand-over of its tickets and mail, must happen before anyone takes its
	// address.
	if s.Agents.Pending(address) {
		return org.Member{}, fmt.Errorf("%w: %s is still finishing its turn after a change; try again once it has stopped", ErrExists, address)
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

	if errors.Is(err, org.ErrDirectorExists) {
		return fmt.Errorf("%w: %w", ErrExists, err)
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

// reloadAndApply reloads the organization and applies the change to the
// agent at address, with opts. Any other agent whose file changed since the
// last reload (edited by hand) is applied too, with the default options, so
// that the running agents always match the files.
func (s *Service) reloadAndApply(ctx context.Context, address string, opts runtime.ApplyOptions) error {
	before := s.Org.Current()

	// A reload can only fail here if another file was broken by hand meanwhile.
	after, err := s.Org.Reload()
	if err != nil {
		return fmt.Errorf("saved, but the organization cannot be reloaded (fix .nobody/ by hand): %w", err)
	}

	if s.OnReload != nil {
		s.OnReload(before, after)
	}

	// address is "" for a change that is not about one agent (a team or the
	// project prompt): the agents it concerns are among the changed ones, and
	// get opts.Now.
	if address != "" {
		if err := s.Agents.Apply(ctx, address, opts); err != nil {
			return fmt.Errorf("saved, but %s cannot be restarted: %w", address, err)
		}
	}

	return s.applyOthers(ctx, address, before, after, address == "" && opts.Now)
}

// applyOthers applies every agent changed between before and after, except
// the one at address, with a fresh session when its prompt changed, and at
// once when now.
func (s *Service) applyOthers(ctx context.Context, address string, before, after *org.Org, now bool) error {
	var errs []error

	for _, other := range changedAgents(before, after) {
		if other == address {
			continue
		}

		oldMember, wasThere := before.Member(other)
		newMember, isThere := after.Member(other)
		fresh := wasThere && isThere &&
			(needsFreshSession(oldMember.Agent.Definition, newMember.Agent.Definition) ||
				sharedPrompt(before, oldMember) != sharedPrompt(after, newMember))

		if err := s.Agents.Apply(ctx, other, runtime.ApplyOptions{Fresh: fresh, Now: now}); err != nil {
			errs = append(errs, fmt.Errorf("apply the hand-made change to %s: %w", other, err))
		}
	}

	return errors.Join(errs...)
}

// needsFreshSession reports whether a definition change needs a new Claude
// Code session. A resumed session keeps the system prompt it started with
// (Claude Code ignores a new --append-system-prompt on --resume), and the
// model does not reliably accept new instructions handed over in a message
// (checked by task contract): a new role or new instructions only take
// effect in a fresh session. Model, permissions, tools and MCP servers are
// launch flags, which a resumed session does follow.
func needsFreshSession(before, after catalog.Definition) bool {
	return before.Role != after.Role || strings.TrimSpace(before.Prompt) != strings.TrimSpace(after.Prompt)
}

// sharedPrompt is what an agent's system prompt holds besides its own
// definition and the org chart: its team's goal and prompt, and the project
// prompt. Like new instructions, a change here needs a fresh session (see
// needsFreshSession). The rest of the org chart is not part of it: the agents
// are told of arrivals and departures by a notice instead (daemon/orgnotice.go).
func sharedPrompt(o *org.Org, m org.Member) string {
	return strings.Join([]string{o.ProjectPrompt, m.Team.Goal, m.Team.Prompt}, "\x00")
}

// changedAgents lists the agents added, removed or changed between two
// snapshots of the organization, a change of their shared prompt included.
func changedAgents(before, after *org.Org) []string {
	var changed []string

	for _, m := range after.Members() {
		old, ok := before.Member(m.Address)
		if !ok || !reflect.DeepEqual(old.Agent, m.Agent) || sharedPrompt(before, old) != sharedPrompt(after, m) {
			changed = append(changed, m.Address)
		}
	}

	for _, m := range before.Members() {
		if _, ok := after.Member(m.Address); !ok {
			changed = append(changed, m.Address)
		}
	}

	return changed
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
