package approval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func request(id string, at time.Time) Request {
	return Request{ID: id, Agent: "dev1@dev", ToolName: "Bash", CreatedAt: at}
}

// waitPending waits until n requests are pending (Ask runs in a goroutine).
func waitPending(t *testing.T, q *Queue, n int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for len(q.Pending()) != n {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want %d", len(q.Pending()), n)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestAskReturnsTheDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision Decision
	}{
		{"allowed", Decision{Allow: true}},
		{"denied with a reason", Decision{Allow: false, Message: "not in the sandbox"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := NewQueue()
			got := make(chan Decision, 1)

			go func() {
				d, err := q.Ask(context.Background(), request("a1", time.Now()))
				if err != nil {
					t.Error(err)
				}

				got <- d
			}()

			waitPending(t, q, 1)

			if err := q.Decide("a1", tt.decision); err != nil {
				t.Fatal(err)
			}

			if d := <-got; d != tt.decision {
				t.Fatalf("decision = %+v, want %+v", d, tt.decision)
			}

			waitPending(t, q, 0)
		})
	}
}

func TestAskWithoutDecisionGivesUp(t *testing.T) {
	t.Parallel()

	q := NewQueue()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := q.Ask(ctx, request("a1", time.Now())); !errors.Is(err, ErrNoDecision) {
		t.Fatalf("error = %v, want ErrNoDecision", err)
	}

	if len(q.Pending()) != 0 {
		t.Fatal("an abandoned request must leave the queue")
	}

	if err := q.Decide("a1", Decision{Allow: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deciding an abandoned request: error = %v, want ErrNotFound", err)
	}
}

func TestPendingIsOldestFirstAndDecidedOnce(t *testing.T) {
	t.Parallel()

	q := NewQueue()

	t0 := time.Now()
	for _, r := range []Request{request("new", t0.Add(time.Second)), request("old", t0)} {
		go q.Ask(context.Background(), r) //nolint:errcheck // decided below
	}

	waitPending(t, q, 2)

	pending := q.Pending()
	if pending[0].ID != "old" || pending[1].ID != "new" {
		t.Fatalf("order = %s, %s; want old, new", pending[0].ID, pending[1].ID)
	}

	if err := q.Decide("old", Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}

	if err := q.Decide("old", Decision{Allow: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second decision: error = %v, want ErrNotFound", err)
	}

	if err := q.Decide("new", Decision{}); err != nil {
		t.Fatal(err)
	}
}
