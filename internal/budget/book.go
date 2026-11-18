package budget

import (
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
