package daemon

import (
	"sync/atomic"
	"testing"
	"time"
)

// A burst of calls is folded into one, after the interval.
func TestThrottle(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	trigger := throttle(20*time.Millisecond, func() { calls.Add(1) })

	for range 10 {
		trigger()
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("%d call(s) before the interval, want 0", got)
	}

	time.Sleep(60 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("%d call(s) after a burst, want 1", got)
	}

	trigger()
	time.Sleep(60 * time.Millisecond)

	if got := calls.Load(); got != 2 {
		t.Fatalf("%d call(s) after a second burst, want 2", got)
	}
}
