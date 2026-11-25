package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/automation"
)

func TestDecisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	d := automation.Decision{
		AskedBy: "dev1@dev", Message: "m1", Question: "Which DB?", Decision: "SQLite", Rationale: "simple",
		Impact: automation.ImpactHigh, DecidedAt: at,
	}

	first, err := s.InsertDecision(ctx, d)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.InsertDecision(ctx, d); !errors.Is(err, automation.ErrDecided) {
		t.Fatalf("second decision on the same question: err = %v, want ErrDecided", err)
	}

	d.Message = "m2"
	if _, err := s.InsertDecision(ctx, d); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListDecisions(ctx)
	if err != nil || len(got) != 2 || got[0].Message != "m2" || got[1].ID != first.ID || !got[1].DecidedAt.Equal(at) {
		t.Fatalf("list = %+v, err %v", got, err)
	}

	if err := s.MarkDecisionsSeen(ctx, []int64{first.ID}); err != nil {
		t.Fatal(err)
	}

	got, _ = s.ListDecisions(ctx)
	if got[0].Seen || !got[1].Seen {
		t.Fatalf("only the first decision should be seen: %+v", got)
	}

	if err := s.MarkDecisionsSeen(ctx, nil); err != nil {
		t.Fatal(err)
	}

	if got, _ = s.ListDecisions(ctx); !got[0].Seen {
		t.Fatal("no IDs marks every decision seen")
	}
}
