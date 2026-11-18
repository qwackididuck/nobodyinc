// Package runtime runs the agents: one Claude Code session per agent, inside a
// pseudo-terminal, and keeps track of what each agent is doing.
package runtime

import (
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/hook"
)

// Status is what an agent is doing right now.
type Status string

// The life of an agent: stopped → starting → working ⇄ idle → exited, with
// working ⇄ awaiting_approval while a permission request is pending, and
// budget_hold instead of idle while a budget holds it (ADR-0013).
const (
	StatusStopped  Status = "stopped"  // not launched
	StatusStarting Status = "starting" // process launched, no hook event yet
	StatusWorking  Status = "working"  // in the middle of a turn
	StatusIdle     Status = "idle"     // turn finished, nothing left to do
	StatusExited   Status = "exited"   // process ended
	// StatusAwaitingApproval: blocked on a permission only the human can give.
	StatusAwaitingApproval Status = "awaiting_approval"
	// StatusBudgetHold: a budget at its threshold holds it; its session is
	// kept, it is never woken up and gets no work (ADR-0013, point 4).
	StatusBudgetHold Status = "budget_hold"
)

// Statuses records the status of every agent, and the Claude Code session it
// runs (learnt from its hooks). It is safe for concurrent use.
type Statuses struct {
	mu       sync.Mutex
	statuses map[string]Status
	sessions map[string]string
	// OnIdle, when set, is called when an agent becomes idle, outside the lock.
	OnIdle func(address string)
	// OnChange, when set, is called when an agent's status actually changes
	// (not when the same status is recorded again), outside the lock.
	OnChange func(address string, st Status)
	// OnSession, when set, is called when a hook reports a session ID that
	// differs from the agent's known one, outside the lock (the daemon
	// remembers it across restarts, Supervisor.RememberSession).
	OnSession func(address, sessionID string)
}

// NewStatuses returns a registry where every agent is stopped.
func NewStatuses() *Statuses {
	return &Statuses{statuses: map[string]Status{}, sessions: map[string]string{}}
}

// Get returns the status of an agent.
func (s *Statuses) Get(address string) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.statuses[address]; ok {
		return st
	}

	return StatusStopped
}

// Set records the status of an agent.
func (s *Statuses) Set(address string, st Status) {
	s.mu.Lock()

	previous, known := s.statuses[address]
	if !known {
		previous = StatusStopped
	}

	s.statuses[address] = st
	s.mu.Unlock()

	if previous != st && s.OnChange != nil {
		s.OnChange(address, st)
	}

	if st == StatusIdle && s.OnIdle != nil {
		s.OnIdle(address)
	}
}

// Session returns the Claude Code session ID of an agent, or "" if unknown.
func (s *Statuses) Session(address string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sessions[address]
}

// Busy reports whether an agent is in the middle of a turn.
func (s *Statuses) Busy(address string) bool {
	st := s.Get(address)

	return st == StatusWorking || st == StatusAwaitingApproval
}

// ObserveHeldStop records the end of a turn of an agent a budget holds: it
// is on budget hold, not idle, so nothing reacts as if it were free
// (OnIdle is not called).
func (s *Statuses) ObserveHeldStop(address string, ev hook.Event) {
	s.rememberSession(address, ev)
	s.Set(address, StatusBudgetHold)
}

// ObserveHook updates an agent's status from a hook event. delivered is the
// number of messages handed over in response to that event.
//
// A Stop with nothing delivered means the agent really ends its turn: it is
// idle. Any other event means it is still working.
func (s *Statuses) ObserveHook(address string, ev hook.Event, delivered int) {
	s.rememberSession(address, ev)

	switch {
	case ev.Name == hook.EventStop && delivered == 0:
		s.Set(address, StatusIdle)
	case ev.Name == hook.EventPermissionRequest:
		s.Set(address, StatusAwaitingApproval)
	default:
		s.Set(address, StatusWorking)
	}
}

// ObserveSessionStart records the session an agent starts or resumes, from
// its SessionStart hook. Its status does not change, unless settled is set:
// a session resumed silently runs no turn and settles idle, or on budget
// hold (Supervisor.SessionStarted).
func (s *Statuses) ObserveSessionStart(address string, ev hook.Event, settled Status) {
	s.rememberSession(address, ev)

	if settled != "" {
		s.Set(address, settled)
	}
}

// rememberSession records the session an agent runs, from a hook event.
func (s *Statuses) rememberSession(address string, ev hook.Event) {
	if ev.SessionID == "" {
		return
	}

	s.mu.Lock()
	changed := s.sessions[address] != ev.SessionID
	s.sessions[address] = ev.SessionID
	s.mu.Unlock()

	if changed && s.OnSession != nil {
		s.OnSession(address, ev.SessionID)
	}
}
