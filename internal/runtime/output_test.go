package runtime

import "testing"

// When the agent's process ends, its terminal viewers are let go, so that
// the control room reconnects to the next session instead of staying on a
// terminal that will never move again; a late viewer still gets the backlog.
func TestOutputCloseLetsViewersGo(t *testing.T) {
	t.Parallel()

	o := NewOutput()
	_, _ = o.Write([]byte("hello"))

	_, live, cancel := o.Subscribe()
	defer cancel()

	o.Close()

	if _, open := <-live; open {
		t.Fatal("a viewer's channel should be closed once the process ended")
	}

	backlog, late, lateCancel := o.Subscribe()
	defer lateCancel()

	if string(backlog) != "hello" {
		t.Errorf("late backlog = %q, want hello", backlog)
	}

	if _, open := <-late; open {
		t.Fatal("a viewer arriving after the end should get a closed channel")
	}

	cancel() // canceling after Close is harmless
}
