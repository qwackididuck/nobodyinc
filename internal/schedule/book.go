package schedule

import (
	"fmt"
	"slices"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Book holds the project's schedules and is the only writer of
// schedules.yaml. Each change reads the file again first, so a change made
// by hand is picked up at the next change made through Nobody Inc (and kept),
// then writes the whole file atomically and calls OnChange.
type Book struct {
	projectDir string
	org        org.Source
	// onChange is called after each write, with the lock held so that calls
	// arrive in order; it must not call back into the book.
	onChange func([]Schedule)

	mu        sync.Mutex
	schedules []Schedule
}

// OpenBook reads the project's schedules.
func OpenBook(projectDir string, o org.Source, onChange func([]Schedule)) (*Book, error) {
	schedules, err := Load(projectDir)
	if err != nil {
		return nil, err
	}

	return &Book{projectDir: projectDir, org: o, onChange: onChange, schedules: schedules}, nil
}

// List returns the schedules, in the file's order.
func (b *Book) List() []Schedule {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.schedules)
}

// Get returns one schedule.
func (b *Book) Get(id string) (Schedule, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return Find(b.schedules, id)
}

// Create adds an enabled schedule; its target must exist.
func (b *Book) Create(s Schedule) (Schedule, error) {
	s.Enabled = true

	if err := s.check(b.org.Current()); err != nil {
		return Schedule{}, err
	}

	err := b.change(func(schedules []Schedule) ([]Schedule, error) {
		if _, ok := Find(schedules, s.ID); ok {
			return nil, fmt.Errorf("%w: %q", ErrExists, s.ID)
		}

		return append(schedules, s), nil
	})

	return s, err
}

// Update replaces a schedule, keeping its id and whether it is enabled; its
// target must exist.
func (b *Book) Update(id string, s Schedule) (Schedule, error) {
	s.ID = id

	if err := s.check(b.org.Current()); err != nil {
		return Schedule{}, err
	}

	err := b.change(func(schedules []Schedule) ([]Schedule, error) {
		i := slices.IndexFunc(schedules, func(x Schedule) bool { return x.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
		}

		s.Enabled = schedules[i].Enabled
		schedules[i] = s

		return schedules, nil
	})

	return s, err
}

// SetEnabled pauses or resumes a schedule (or disables it, when its target
// is gone). Resuming checks that the target exists.
func (b *Book) SetEnabled(id string, enabled bool) (Schedule, error) {
	var updated Schedule

	err := b.change(func(schedules []Schedule) ([]Schedule, error) {
		i := slices.IndexFunc(schedules, func(x Schedule) bool { return x.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
		}

		if enabled {
			if err := schedules[i].CheckTarget(b.org.Current()); err != nil {
				return nil, err
			}
		}

		schedules[i].Enabled = enabled
		updated = schedules[i]

		return schedules, nil
	})

	return updated, err
}

// Delete removes a schedule.
func (b *Book) Delete(id string) error {
	return b.change(func(schedules []Schedule) ([]Schedule, error) {
		i := slices.IndexFunc(schedules, func(x Schedule) bool { return x.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
		}

		return slices.Delete(schedules, i, i+1), nil
	})
}

// check validates a schedule on its own first, so that its own mistakes are
// reported before its target's.
func (s Schedule) check(o *org.Org) error {
	if err := s.Check(); err != nil {
		return err
	}

	return s.CheckTarget(o)
}

// change reads the file, applies edit, writes the result and announces it.
func (b *Book) change(edit func([]Schedule) ([]Schedule, error)) error {
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

	if err := Save(b.projectDir, changed); err != nil {
		return err
	}

	b.schedules = changed

	if b.onChange != nil {
		b.onChange(slices.Clone(changed))
	}

	return nil
}
