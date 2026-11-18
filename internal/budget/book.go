package budget

import (
	"fmt"
	"maps"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Book holds the project's budget settings and is the only writer of
// budgets.yaml, as schedule.Book is of schedules.yaml (ADR-0004). Each change
// reads the file again first, so a change made by hand is picked up at the
// next change made through Nobody Inc (and kept), drops the caps of teams
// and agents that no longer exist, then writes the whole file atomically and
// calls OnChange.
type Book struct {
	projectDir string
	org        org.Source
	// onChange is called after each write, with the lock held so that calls
	// arrive in order; it must not call back into the book.
	onChange func(Settings)

	mu       sync.Mutex
	settings Settings
}

// OpenBook reads the project's budget settings. An invalid file is an error:
// the daemon does not start, as with an invalid org.yaml.
func OpenBook(projectDir string, o org.Source, onChange func(Settings)) (*Book, error) {
	s, err := Load(projectDir)
	if err != nil {
		return nil, err
	}

	return &Book{projectDir: projectDir, org: o, onChange: onChange, settings: s}, nil
}

// Get returns the settings.
func (b *Book) Get() Settings {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.settings
}

// Enabled reports whether budgets are on for the project.
func (b *Book) Enabled() bool {
	return b.Get().Enabled
}

// Replace writes new settings; every capped team and agent must exist.
func (b *Book) Replace(s Settings) (Settings, error) {
	if err := s.Check(); err != nil {
		return Settings{}, err
	}

	if err := s.CheckOrg(b.org.Current()); err != nil {
		return Settings{}, err
	}

	err := b.change(func(Settings) (Settings, error) { return s, nil })

	return b.Get(), err
}

// Cap returns the cap of an agent, a team or the staff, if set.
func (s Settings) Cap(scope Scope) (Cap, bool) {
	switch scope.Kind {
	case KindStaff:
		if s.Staff != nil {
			return *s.Staff, true
		}
	case KindTeam:
		c, ok := s.Teams[scope.ID]

		return c, ok
	case KindAgent:
		c, ok := s.Agents[scope.ID]

		return c, ok
	case KindTicket, KindFeature:
	}

	return Cap{}, false
}

// withCap returns the settings with an agent, team or staff cap set.
func (s Settings) withCap(scope Scope, c Cap) Settings {
	switch scope.Kind {
	case KindStaff:
		s.Staff = &c
	case KindTeam:
		s.Teams = maps.Clone(s.Teams)
		if s.Teams == nil {
			s.Teams = map[string]Cap{}
		}

		s.Teams[scope.ID] = c
	case KindAgent:
		s.Agents = maps.Clone(s.Agents)
		if s.Agents == nil {
			s.Agents = map[string]Cap{}
		}

		s.Agents[scope.ID] = c
	case KindTicket, KindFeature:
	}

	return s
}

// SetCap sets an agent, team or staff cap (an accepted proposal), keeping
// the rest of the file as it is.
func (b *Book) SetCap(scope Scope, c Cap) (Settings, error) {
	if !scope.IsCap() {
		return Settings{}, fmt.Errorf("%w: %s is not an agent, team or staff budget", ErrInvalid, scope)
	}

	candidate := Settings{Threshold: DefaultThreshold}.withCap(scope, c)
	if err := candidate.Check(); err != nil {
		return Settings{}, err
	}

	if err := candidate.CheckOrg(b.org.Current()); err != nil {
		return Settings{}, err
	}

	err := b.change(func(s Settings) (Settings, error) { return s.withCap(scope, c), nil })

	return b.Get(), err
}

// RaiseCap raises an existing cap by usd: an extension of a cumulative cap
// (ADR-0013, point 6).
func (b *Book) RaiseCap(scope Scope, usd float64) (Settings, error) {
	err := b.change(func(s Settings) (Settings, error) {
		c, ok := s.Cap(scope)
		if !ok {
			return Settings{}, fmt.Errorf("%w: %s has no cap", ErrConflict, scope)
		}

		c.LimitUSD += usd

		return s.withCap(scope, c), nil
	})

	return b.Get(), err
}

// change reads the file, applies edit, drops the caps nobody can use any
// more, writes the result and announces it.
func (b *Book) change(edit func(Settings) (Settings, error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	current, err := Load(b.projectDir)
	if err != nil {
		return err
	}

	changed, err := edit(current)
	if err != nil {
		return err
	}

	changed = changed.Pruned(b.org.Current())

	if err := Save(b.projectDir, changed); err != nil {
		return err
	}

	b.settings = changed

	if b.onChange != nil {
		b.onChange(changed)
	}

	return nil
}
