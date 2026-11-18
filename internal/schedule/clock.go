package schedule

import (
	"sync"
	"time"
)

// Clock tells the time and makes timers. The daemon uses the machine's
// (RealClock); tests use a ManualClock, so they never wait.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer fires once on C, unless stopped.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// RealClock is the machine's clock. It tells the time in UTC, as everything
// stored does; recurrences read it in the machine's time zone themselves.
type RealClock struct{}

// Now returns the current time, in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// NewTimer returns a timer that fires after d.
func (RealClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

// ManualClock is a clock that only moves when told to (Advance), for tests.
// Its timers fire when the time they wait for is reached.
type ManualClock struct {
	mu      sync.Mutex
	changed *sync.Cond
	now     time.Time
	timers  []*manualTimer
}

// NewManualClock returns a clock showing now.
func NewManualClock(now time.Time) *ManualClock {
	c := &ManualClock{now: now}
	c.changed = sync.NewCond(&c.mu)

	return c
}

// Now returns the clock's time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// NewTimer returns a timer that fires once the clock reaches now + d.
func (c *ManualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := &manualTimer{clock: c, at: c.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now

		return t
	}

	c.timers = append(c.timers, t)
	c.changed.Broadcast()

	return t
}

// Advance moves the clock forward and fires the timers that are due.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)

	pending := c.timers[:0]

	for _, t := range c.timers {
		if t.at.After(c.now) {
			pending = append(pending, t)

			continue
		}

		t.ch <- c.now
	}

	c.timers = pending
	c.changed.Broadcast()
}

// WaitForTimers blocks until at least n timers are waiting: a test calls it
// before Advance, so that the code under test has armed its timer.
func (c *ManualClock) WaitForTimers(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for len(c.timers) < n {
		c.changed.Wait()
	}
}

type manualTimer struct {
	clock *ManualClock
	at    time.Time
	ch    chan time.Time
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }

func (t *manualTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, x := range c.timers {
		if x == t {
			c.timers = append(c.timers[:i], c.timers[i+1:]...)
			c.changed.Broadcast()

			return true
		}
	}

	return false
}
