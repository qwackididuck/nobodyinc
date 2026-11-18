package work_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func usd(v float64) *float64 { return new(v) }

// Inside a feature with a budget, every ticket needs one, at most what the
// feature has left; a canceled ticket gives back what it did not spend
// (ADR-0013, point 3).
func TestFeatureBudgetIsAllocatedToTickets(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	f, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Orders", Budget: work.Budget{USD: usd(10)}})
	if err != nil {
		t.Fatal(err)
	}

	newTicket := func(b work.Budget) (work.Ticket, error) {
		return s.Create(ctx, org.HumanAddress, work.Draft{Title: "Part", Team: "dev", Budget: b})
	}

	if _, err := newTicket(work.Budget{}); !errors.Is(err, work.ErrInvalid) {
		t.Fatalf("a ticket without a budget in a feature with one: %v, want ErrInvalid", err)
	}

	a, err := newTicket(work.Budget{USD: usd(6)})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := newTicket(work.Budget{USD: usd(5)}); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("$5 with $4 left: %v, want ErrConflict", err)
	}

	b, err := newTicket(work.Budget{USD: usd(4)})
	if err != nil {
		t.Fatal(err)
	}

	assertLeft(t, s, f.ID, 0)

	// B spends 0.5 and is canceled: it gives back the 3.5 it did not spend.
	spend(t, s, b, 0.5)

	if _, err := s.SetStatus(ctx, org.HumanAddress, b.ID, work.StatusCanceled); err != nil {
		t.Fatal(err)
	}

	assertLeft(t, s, f.ID, 3.5)

	// A may grow by what is left, not more.
	if _, err := s.SetBudget(ctx, org.HumanAddress, a.ID, work.Budget{USD: new(9.6)}); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("$9.60 for A with $3.50 left beside its $6: %v, want ErrConflict", err)
	}

	if _, err := s.SetBudget(ctx, org.HumanAddress, a.ID, work.Budget{USD: new(9.5)}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetBudget(ctx, org.HumanAddress, a.ID, work.Budget{}); !errors.Is(err, work.ErrInvalid) {
		t.Fatalf("removing A's budget in a feature with one: %v, want ErrInvalid", err)
	}

	if _, err := s.SetBudget(ctx, "god", a.ID, work.Budget{USD: usd(1)}); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("the director changed a ticket's budget: %v, want ErrForbidden", err)
	}

	if _, err := s.SetBudget(ctx, org.HumanAddress, b.ID, work.Budget{USD: usd(1)}); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("a canceled ticket's budget changed: %v, want ErrConflict", err)
	}

	got, err := s.Store.GetTicket(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.USD == nil || *got.USD != 9.5 {
		t.Fatalf("A's budget = %v, want 9.5", got.USD)
	}
}

// While budgets are turned off, no budget is taken, and none is required.
func TestBudgetsTurnedOff(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	if _, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Orders", Budget: work.Budget{USD: usd(10)}}); err != nil {
		t.Fatal(err)
	}

	s.BudgetsEnabled = func() bool { return false }

	if _, err := s.Create(ctx, org.HumanAddress, work.Draft{Title: "Part", Team: "dev", Budget: work.Budget{USD: usd(1)}}); !errors.Is(err, work.ErrInvalid) {
		t.Fatalf("a budget while budgets are off: %v, want ErrInvalid", err)
	}

	if _, err := s.Create(ctx, org.HumanAddress, work.Draft{Title: "Part", Team: "dev"}); err != nil {
		t.Fatalf("no budget required while budgets are off: %v", err)
	}

	if _, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "More", Budget: work.Budget{USD: usd(1)}}); !errors.Is(err, work.ErrInvalid) {
		t.Fatalf("a feature budget while budgets are off: %v, want ErrInvalid", err)
	}
}

func TestBudgetChecks(t *testing.T) {
	t.Parallel()

	threshold := func(v int) *int { return &v }

	tests := []struct {
		name string
		b    work.Budget
	}{
		{"zero", work.Budget{USD: usd(0)}},
		{"negative", work.Budget{USD: usd(-1)}},
		{"a threshold alone", work.Budget{Threshold: threshold(50)}},
		{"threshold 0", work.Budget{USD: usd(1), Threshold: threshold(0)}},
		{"threshold 101", work.Budget{USD: usd(1), Threshold: threshold(101)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newService(t)

			if _, err := s.Create(context.Background(), org.HumanAddress, work.Draft{Title: "x", Team: "dev", Budget: tt.b}); !errors.Is(err, work.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

// The director proposes a feature's budget; the human keeps it or replaces
// it when starting the feature.
func TestProposedFeatureBudget(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	f, err := s.ProposeFeature(ctx, "god", work.FeatureDraft{Title: "Orders", Budget: work.Budget{USD: usd(20)}})
	if err != nil {
		t.Fatal(err)
	}

	started, err := s.StartProposedFeature(ctx, org.HumanAddress, f.ID, &work.Budget{USD: usd(15)})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Store.GetFeature(ctx, started.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.USD == nil || *got.USD != 15 {
		t.Fatalf("started feature budget = %v, want the human's 15", got.USD)
	}
}

func assertLeft(t *testing.T, s *work.Service, featureID string, want float64) {
	t.Helper()

	f, err := s.Store.GetFeature(context.Background(), featureID)
	if err != nil {
		t.Fatal(err)
	}

	if math.Abs(f.Left()-want) > 1e-9 {
		t.Fatalf("%s has %.2f left to allocate (allocated %.2f), want %.2f", f.ID, f.Left(), f.AllocatedUSD, want)
	}
}

// spend records what a ticket spent, as the meter would.
func spend(t *testing.T, s *work.Service, tk work.Ticket, amount float64) {
	t.Helper()

	st, ok := s.Store.(*store.Store)
	if !ok {
		t.Fatal("the service's store is not a *store.Store")
	}

	_, err := st.InsertSpend(context.Background(), budget.Record{
		At: time.Now().UTC(), Month: budget.Month(time.Now()), Source: budget.SourceStatusLine, AmountUSD: amount,
		Agent: "dev1@dev", Team: "dev", Ticket: tk.ID, Feature: tk.Feature,
	})
	if err != nil {
		t.Fatal(err)
	}
}
