// Package automation holds the project's automatic mode: whether the daemon
// answers the agents' permission requests itself instead of leaving them to
// the human (docs/automation.md).
//
// The level lives in the daemon's memory only, on purpose: closing the
// project's window or restarting the daemon puts the project back to Off, so
// a dangerous mode is never left on by forgetting about it.
package automation

import (
	"errors"
	"fmt"
	"sync"
)

// Level is how much the daemon decides on its own.
type Level string

const (
	// Off leaves every permission request to the human.
	Off Level = "off"
	// Auto allows every permission request. Claude Code's deny rules still
	// win: they are evaluated before a request is ever made (ADR-0004).
	Auto Level = "auto"
	// Nobody is Auto, and the director also answers, in the human's place,
	// the questions the agents ask them (docs/automation.md).
	Nobody Level = "nobody"
)

// ErrUnknownLevel is returned by Set for a level that does not exist.
var ErrUnknownLevel = errors.New("unknown automation level")

// Mode is the project's current level. It is safe for concurrent use.
type Mode struct {
	mu    sync.Mutex
	level Level
	// OnChange, when set, is called after the level actually changes.
	OnChange func()
}

// New returns a Mode that is Off.
func New() *Mode { return &Mode{level: Off} }

// Level returns the current level.
func (m *Mode) Level() Level {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.level
}

// AutoApprove tells whether permission requests are allowed without the human.
func (m *Mode) AutoApprove() bool { return m.Level() != Off }

// DirectorDecides tells whether the director answers the agents' questions
// instead of the human.
func (m *Mode) DirectorDecides() bool { return m.Level() == Nobody }

// Set changes the level and reports whether it differed from the previous one.
func (m *Mode) Set(l Level) (bool, error) {
	if l != Off && l != Auto && l != Nobody {
		return false, fmt.Errorf("%w: %q", ErrUnknownLevel, l)
	}

	m.mu.Lock()
	changed := m.level != l
	m.level = l
	m.mu.Unlock()

	if changed && m.OnChange != nil {
		m.OnChange()
	}

	return changed, nil
}
