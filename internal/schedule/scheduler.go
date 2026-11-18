package schedule

import (
	"context"
	"sort"
	"sync"
	"time"
)

// DueFunc is called each time a schedule falls due, with the time it was due.
// It runs on the scheduler's goroutine: it should not block for long, and
// may call Set.
type DueFunc func(ctx context.Context, s Schedule, at time.Time)

// Scheduler calls Due when an enabled schedule falls due, while Run runs. It
// keeps one timer, on the earliest next occurrence of all schedules, armed
// again whenever the schedules change.
//
// A schedule is armed when it first appears enabled in Set (daemon start,
// creation, resume) or when its definition changes (edit): an interval counts
// from there, and occurrences missed before are never caught up. If the
// machine sleeps past several occurrences, only one is due when it wakes up.
type Scheduler struct {
	clock Clock
	due   DueFunc

	mu      sync.Mutex
	entries map[string]*armed
	wake    chan struct{}
}

// armed is an enabled schedule with its next occurrence.
type armed struct {
	schedule Schedule
	armedAt  time.Time
	next     time.Time
}

// NewScheduler returns a scheduler with no schedules; give them with Set.
func NewScheduler(clock Clock, due DueFunc) *Scheduler {
	return &Scheduler{clock: clock, due: due, entries: map[string]*armed{}, wake: make(chan struct{}, 1)}
}

// Set gives the current schedules. Disabled and removed ones are dropped;
// new, resumed and edited ones are armed from now; the others keep their
// next occurrence.
func (s *Scheduler) Set(schedules []Schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	entries := map[string]*armed{}

	for _, sc := range schedules {
		if !sc.Enabled {
			continue
		}

		if e, ok := s.entries[sc.ID]; ok && e.schedule == sc {
			entries[sc.ID] = e

			continue
		}

		if next := sc.Next(now, now); !next.IsZero() {
			entries[sc.ID] = &armed{schedule: sc, armedAt: now, next: next}
		}
	}

	s.entries = entries

	// Wake the loop up so that it waits for the new earliest occurrence.
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Next returns a schedule's next occurrence; false when it is not armed
// (disabled, or unknown).
func (s *Scheduler) Next(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		return time.Time{}, false
	}

	return e.next, true
}

// Run waits for occurrences and calls Due for each, until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		var (
			timer Timer
			fired <-chan time.Time
		)

		if next, ok := s.earliest(); ok {
			timer = s.clock.NewTimer(next.Sub(s.clock.Now()))
			fired = timer.C()
		}

		select {
		case <-ctx.Done():
			stop(timer)

			return
		case <-s.wake:
			stop(timer)
		case <-fired:
			s.fireDue(ctx)
		}
	}
}

func stop(t Timer) {
	if t != nil {
		t.Stop()
	}
}

// earliest returns the earliest next occurrence of all armed schedules.
func (s *Scheduler) earliest() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var first time.Time

	for _, e := range s.entries {
		if first.IsZero() || e.next.Before(first) {
			first = e.next
		}
	}

	return first, !first.IsZero()
}

// fireDue calls Due for every schedule whose occurrence is reached, in the
// order they fell due, after moving each to its following occurrence.
func (s *Scheduler) fireDue(ctx context.Context) {
	type occurrence struct {
		schedule Schedule
		at       time.Time
	}

	var due []occurrence

	s.mu.Lock()
	now := s.clock.Now()

	for id, e := range s.entries {
		if e.next.After(now) {
			continue
		}

		due = append(due, occurrence{schedule: e.schedule, at: e.next})

		if e.next = e.schedule.Next(e.armedAt, now); e.next.IsZero() {
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()

	sort.Slice(due, func(i, j int) bool {
		if due[i].at.Equal(due[j].at) {
			return due[i].schedule.ID < due[j].schedule.ID
		}

		return due[i].at.Before(due[j].at)
	})

	for _, o := range due {
		s.due(ctx, o.schedule, o.at)
	}
}
