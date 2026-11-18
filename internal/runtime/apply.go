package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ApplyOptions tune how a definition change reaches a running agent.
type ApplyOptions struct {
	// Fresh starts a new Claude Code session instead of resuming the agent's
	// current one: the agent forgets everything ("restart from scratch").
	Fresh bool
	// Now applies the change at once, even in the middle of a turn, e.g. to
	// stop an agent that is stuck.
	Now bool
}

// pendingChange is a change waiting for an agent to end its turn.
type pendingChange struct {
	// ctx bounds the restarted process; see Apply.
	ctx  context.Context //nolint:containedctx // kept until the agent is idle, then used to start it
	opts ApplyOptions
}

// Apply brings an agent in line with its current definition, after the
// organization has changed (docs/agents.md, "Lifecycle"):
//
//   - an agent that should run (it is in the organization and not on leave)
//     is restarted, resuming its Claude Code session unless opts.Fresh, so
//     that it keeps its memory and gets its new definition;
//   - an agent on leave, or no longer in the organization, is stopped.
//
// A change never interrupts a turn: if the agent is busy, it waits until the
// agent is idle (OnIdle), or until its process exits. opts.Now skips the wait.
// A newer change for the same agent replaces a waiting one.
//
// ctx bounds the restarted agent's process, so it must outlive the caller's
// request (e.g. context.WithoutCancel of the daemon's context).
func (s *Supervisor) Apply(ctx context.Context, address string, opts ApplyOptions) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()

		return fmt.Errorf("%w: %s", ErrStopping, address)
	}

	if s.pending == nil {
		s.pending = map[string]pendingChange{}
	}

	s.pending[address] = pendingChange{ctx: ctx, opts: opts}
	_, running := s.agents[address]
	s.mu.Unlock()

	if running && !opts.Now && s.midTurn(address) {
		s.Logger.Info("agent change waits for the end of its turn", "agent", address)

		return nil
	}

	return s.applyPending(address)
}

// Pending reports whether a change waits for an agent to end its turn.
func (s *Supervisor) Pending(address string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.pending[address]

	return ok
}

// takePending removes the change waiting for an agent, and reports whether
// there was one.
func (s *Supervisor) takePending(address string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.pending[address]
	delete(s.pending, address)

	return ok
}

// midTurn reports whether a running agent is not idle yet. A starting agent
// counts: its onboarding turn is under way.
func (s *Supervisor) midTurn(address string) bool {
	switch s.Statuses.Get(address) {
	case StatusStarting, StatusWorking, StatusAwaitingApproval:
		return true
	case StatusStopped, StatusIdle, StatusExited, StatusBudgetHold:
		return false
	default:
		return false
	}
}

// applyIfPending applies a waiting change in the background, and reports
// whether there was one. It is called when the agent becomes idle or exits,
// from places that must not block (a hook request, the process watcher).
func (s *Supervisor) applyIfPending(address string) bool {
	if !s.Pending(address) {
		return false
	}

	// No longer idle from now on: mail arriving before the restart must not
	// wake up (and be handed to) the process about to be stopped.
	s.Statuses.Set(address, StatusStarting)

	go func() {
		if err := s.applyPending(address); err != nil {
			s.Logger.Error("apply agent change", "agent", address, "err", err)
		}
	}()

	return true
}

// applyPending takes the waiting change of an agent, if any, and applies it:
// stop, then start again (resumed or fresh) if the agent should run.
func (s *Supervisor) applyPending(address string) error {
	s.mu.Lock()
	change, ok := s.pending[address]
	delete(s.pending, address)
	s.mu.Unlock()

	if !ok {
		return nil // already applied by a concurrent call
	}

	// The session is read before stopping: it is the one to continue.
	resume := ""
	if !change.opts.Fresh {
		resume = s.Statuses.Session(address)
	}

	if err := s.Stop(address); err != nil {
		return err
	}

	member, inOrg := s.Org.Current().Member(address)
	if !inOrg || member.Agent.OnLeave {
		s.Statuses.Set(address, StatusStopped)
		s.Logger.Info("agent stopped after a change", "agent", address, "in_org", inOrg)

		if s.OnRetired != nil {
			s.OnRetired(address)
		}

		return nil
	}

	err := s.start(change.ctx, address, resume)
	if errors.Is(err, ErrAlreadyRunning) {
		return nil // started meanwhile, e.g. by a feature restart
	}

	return err
}
