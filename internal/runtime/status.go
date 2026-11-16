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
// working ⇄ awaiting_approval while a permission request is pending.
const (
	StatusStopped  Status = "stopped"  // not launched
	StatusStarting Status = "starting" // process launched, no hook event yet
	StatusWorking  Status = "working"  // in the middle of a turn
	StatusIdle     Status = "idle"     // turn finished, nothing left to do
	StatusExited   Status = "exited"   // process ended
	// StatusAwaitingApproval: blocked on a permission only the human can give.
	StatusAwaitingApproval Status = "awaiting_approval"
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

// ObserveHook updates an agent's status from a hook event. delivered is the
// number of messages handed over in response to that event.
//
// A Stop with nothing delivered means the agent really ends its turn: it is
// idle. Any other event means it is still working.
func (s *Statuses) ObserveHook(address string, ev hook.Event, delivered int) {
	if ev.SessionID != "" {
		s.mu.Lock()
		s.sessions[address] = ev.SessionID
		s.mu.Unlock()
	}

	switch {
	case ev.Name == hook.EventStop && delivered == 0:
		s.Set(address, StatusIdle)
	case ev.Name == hook.EventPermissionRequest:
		s.Set(address, StatusAwaitingApproval)
	default:
		s.Set(address, StatusWorking)
	}
}
