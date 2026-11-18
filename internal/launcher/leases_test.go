package launcher

import (
	"errors"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

const testGrace = 15 * time.Second

// newTestLeases returns leases on a manual clock, and the channel of the
// projects whose hold ended.
func newTestLeases() (*leases, *schedule.ManualClock, chan string) {
	clock := schedule.NewManualClock(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))
	expired := make(chan string, 8)

	return newLeases(clock, testGrace, func(p string) { expired <- p }), clock, expired
}

func noop() {}

func TestLeasesOneWindowPerProject(t *testing.T) {
	t.Parallel()

	ls, _, _ := newTestLeases()

	if _, err := ls.acquire("shop", "w1", false, noop); err != nil {
		t.Fatal(err)
	}

	if !ls.held("shop") {
		t.Fatal("shop should be held")
	}

	if _, err := ls.acquire("shop", "w2", false, noop); !errors.Is(err, errHeld) {
		t.Fatalf("second window: err = %v, want errHeld", err)
	}

	if _, err := ls.acquire("blog", "w2", false, noop); err != nil {
		t.Fatalf("another project is free: %v", err)
	}
}

func TestLeasesTakeover(t *testing.T) {
	t.Parallel()

	ls, _, expired := newTestLeases()
	taken := make(chan struct{})

	first, err := ls.acquire("shop", "w1", false, func() { close(taken) })
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ls.acquire("shop", "w2", true, noop); err != nil {
		t.Fatalf("takeover refused: %v", err)
	}

	select {
	case <-taken:
	case <-time.After(time.Second):
		t.Fatal("the first window was not told")
	}

	// The first window's connection ends: the second one keeps the project.
	ls.disconnect("shop", first)
	ls.release("shop", first)

	if !ls.held("shop") || len(expired) != 0 {
		t.Fatal("the old window's connection ending must not touch the new hold")
	}
}

func TestLeasesSameWindowIDNewConnectionWins(t *testing.T) {
	t.Parallel()

	ls, _, _ := newTestLeases()
	taken := make(chan struct{})

	// A duplicated tab carries the same window id.
	if _, err := ls.acquire("shop", "w1", false, func() { close(taken) }); err != nil {
		t.Fatal(err)
	}

	if _, err := ls.acquire("shop", "w1", false, noop); err != nil {
		t.Fatalf("same window id refused: %v", err)
	}

	select {
	case <-taken:
	case <-time.After(time.Second):
		t.Fatal("the older connection was not told")
	}
}

func TestLeasesGracePeriod(t *testing.T) {
	t.Parallel()

	t.Run("expires", func(t *testing.T) {
		t.Parallel()

		ls, clock, expired := newTestLeases()

		h, _ := ls.acquire("shop", "w1", false, noop)
		ls.disconnect("shop", h)
		clock.WaitForTimers(1)

		clock.Advance(testGrace - time.Second)

		if !ls.held("shop") {
			t.Fatal("the hold ended before the grace period")
		}

		clock.Advance(time.Second)

		if got := <-expired; got != "shop" || ls.held("shop") {
			t.Fatalf("expired %q, held %v", got, ls.held("shop"))
		}
	})

	t.Run("same window comes back", func(t *testing.T) {
		t.Parallel()

		ls, clock, expired := newTestLeases()

		h, _ := ls.acquire("shop", "w1", false, noop)
		ls.disconnect("shop", h)
		clock.WaitForTimers(1)

		if _, err := ls.acquire("shop", "w1", false, noop); err != nil {
			t.Fatal(err)
		}

		clock.WaitForTimers(0)
		clock.Advance(time.Hour)
		assertNotExpired(t, ls, expired)
	})

	t.Run("another window takes it without takeover", func(t *testing.T) {
		t.Parallel()

		ls, clock, expired := newTestLeases()

		h, _ := ls.acquire("shop", "w1", false, noop)
		ls.disconnect("shop", h)
		clock.WaitForTimers(1)

		if _, err := ls.acquire("shop", "w2", false, noop); err != nil {
			t.Fatalf("a disconnected hold should be free to take: %v", err)
		}

		clock.Advance(time.Hour)
		assertNotExpired(t, ls, expired)

		// The first window is back: the project is open elsewhere now.
		if _, err := ls.acquire("shop", "w1", false, noop); !errors.Is(err, errHeld) {
			t.Fatalf("err = %v, want errHeld", err)
		}
	})
}

func assertNotExpired(t *testing.T, ls *leases, expired chan string) {
	t.Helper()

	select {
	case p := <-expired:
		t.Fatalf("%s expired", p)
	case <-time.After(50 * time.Millisecond):
	}

	if !ls.held("shop") {
		t.Fatal("shop should still be held")
	}
}

func TestLeasesRelease(t *testing.T) {
	t.Parallel()

	ls, _, expired := newTestLeases()

	h, _ := ls.acquire("shop", "w1", false, noop)
	ls.release("shop", h)

	if got := <-expired; got != "shop" || ls.held("shop") {
		t.Fatalf("release: expired %q, held %v", got, ls.held("shop"))
	}
}

func TestLeasesDeleting(t *testing.T) {
	t.Parallel()

	ls, _, expired := newTestLeases()

	h, _ := ls.acquire("shop", "w1", false, noop)

	if err := ls.startDeleting("shop", "w2"); !errors.Is(err, errHeld) {
		t.Fatalf("deleting a project another window holds: err = %v, want errHeld", err)
	}

	if err := ls.startDeleting("shop", "w1"); err != nil {
		t.Fatalf("the holding window may delete it: %v", err)
	}

	if ls.held("shop") || len(expired) != 0 {
		t.Fatal("deleting takes the hold away without the stop callback (the deletion stops the project itself)")
	}

	if _, err := ls.acquire("shop", "w2", true, noop); !errors.Is(err, errDeleting) {
		t.Fatalf("acquire while deleting: err = %v, want errDeleting", err)
	}

	ls.disconnect("shop", h) // the deleting window's connection ends: nothing happens

	ls.finishDeleting("shop")

	if _, err := ls.acquire("shop", "w2", false, noop); err != nil {
		t.Fatalf("acquire after deleting: %v", err)
	}
}
