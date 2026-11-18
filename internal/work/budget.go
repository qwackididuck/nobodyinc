package work

import (
	"context"
	"fmt"
	"math"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Budget is a ticket's or feature's budget (ADR-0013, point 3): an amount
// in list-price USD, Claude Code's estimate, and the percent of it at which
// its agents stop at the end of their turn. Both are optional: no amount
// means no budget, no threshold means the project's.
type Budget struct {
	USD       *float64 `json:"budget_usd,omitempty"`
	Threshold *int     `json:"threshold,omitempty"`
}

// IsSet reports whether there is a budget.
func (b Budget) IsSet() bool {
	return b.USD != nil
}

// check checks a budget on its own.
func (b Budget) check() error {
	if b.USD != nil && (*b.USD <= 0 || math.IsNaN(*b.USD) || math.IsInf(*b.USD, 0)) {
		return fmt.Errorf("%w: a budget must be more than 0", ErrInvalid)
	}

	if b.Threshold != nil {
		if b.USD == nil {
			return fmt.Errorf("%w: a threshold needs a budget", ErrInvalid)
		}

		if *b.Threshold < 1 || *b.Threshold > 100 {
			return fmt.Errorf("%w: a threshold must be between 1 and 100", ErrInvalid)
		}
	}

	return nil
}

// checkBudget refuses a budget while budgets are turned off, and an invalid
// one.
func (s *Service) checkBudget(b Budget) error {
	if b.IsSet() && !s.budgetsEnabled() {
		return fmt.Errorf("%w: budgets are turned off for this project", ErrInvalid)
	}

	return b.check()
}

func (s *Service) budgetsEnabled() bool {
	return s.BudgetsEnabled == nil || s.BudgetsEnabled()
}

// Left is what a feature with a budget has left to allocate to tickets: its
// budget minus what is allocated (Feature.AllocatedUSD). It is 0 for a
// feature without a budget.
func (f Feature) Left() float64 {
	if f.USD == nil {
		return 0
	}

	return *f.USD - f.AllocatedUSD
}

// allocation is what a ticket takes out of its feature's budget: its budget,
// or what it spent when that is more (a turn may overrun); for a canceled
// ticket, only what it spent (ADR-0013, point 3). Feature.AllocatedUSD adds
// these up.
func (t Ticket) allocation() float64 {
	if t.Status == StatusCanceled || t.USD == nil {
		return t.SpentUSD
	}

	return math.Max(*t.USD, t.SpentUSD)
}

// checkFeatureBudget applies the feature's rules to a ticket's budget:
// inside a feature with a budget (and budgets on), every ticket needs one,
// at most what the feature has left; current is the ticket's own
// allocation, given back first when its budget changes.
func (s *Service) checkFeatureBudget(ctx context.Context, featureID string, b Budget, current float64) error {
	if featureID == "" || !s.budgetsEnabled() {
		return nil
	}

	f, err := s.Store.GetFeature(ctx, featureID)
	if err != nil {
		return err
	}

	if !f.IsSet() {
		return nil
	}

	if !b.IsSet() {
		return fmt.Errorf("%w: feature %s has a budget: every ticket needs one (at most $%.2f left to allocate)",
			ErrInvalid, f.ID, f.Left()+current)
	}

	// A cent of tolerance for rounding in the sums.
	if left := f.Left() + current; *b.USD > left+0.005 {
		return fmt.Errorf("%w: feature %s has $%.2f left to allocate, not $%.2f", ErrConflict, f.ID, left, *b.USD)
	}

	return nil
}

// SetBudget changes a ticket's budget. Only the human does, after creation;
// an extension goes through the budget requests (ADR-0013, points 3 and 6).
func (s *Service) SetBudget(ctx context.Context, by, id string, b Budget) (Ticket, error) {
	if by != org.HumanAddress {
		return Ticket{}, fmt.Errorf("%w: only the human changes a ticket's budget", ErrForbidden)
	}

	if !s.budgetsEnabled() {
		return Ticket{}, fmt.Errorf("%w: budgets are turned off for this project", ErrInvalid)
	}

	if err := b.check(); err != nil {
		return Ticket{}, err
	}

	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()

	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Ticket{}, err
	}

	if t.Archived {
		return Ticket{}, fmt.Errorf("%w: %s is archived", ErrConflict, t.ID)
	}

	if t.Status == StatusCanceled {
		return Ticket{}, fmt.Errorf("%w: %s is canceled", ErrConflict, t.ID)
	}

	if err := s.checkFeatureBudget(ctx, t.Feature, b, t.allocation()); err != nil {
		return Ticket{}, err
	}

	t.Budget = b
	t.UpdatedAt = s.now()

	return s.save(ctx, t, "", org.HumanAddress)
}

// ExtendTicketBudget raises a ticket's budget by usd: an extension the human
// granted (ADR-0013, point 6). Inside a feature with a budget it comes out
// of what the feature has left to allocate, and raises the feature's budget
// by what is missing.
func (s *Service) ExtendTicketBudget(ctx context.Context, id string, usd float64) (Ticket, error) {
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()

	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Ticket{}, err
	}

	if !t.IsSet() {
		return Ticket{}, fmt.Errorf("%w: %s has no budget", ErrConflict, t.ID)
	}

	raised := *t.USD + usd

	if t.Feature != "" {
		if err := s.coverFromFeature(ctx, t, raised); err != nil {
			return Ticket{}, err
		}
	}

	t.USD = &raised

	return s.save(ctx, t, "", org.HumanAddress)
}

// coverFromFeature raises the budget of a ticket's feature, if it has one,
// by what it lacks for the ticket's new budget.
func (s *Service) coverFromFeature(ctx context.Context, t Ticket, raised float64) error {
	f, err := s.Store.GetFeature(ctx, t.Feature)
	if err != nil {
		return err
	}

	if !f.IsSet() {
		return nil
	}

	if missing := raised - (f.Left() + t.allocation()); missing > 0 {
		grown := *f.USD + missing
		f.USD = &grown

		return s.Store.UpdateFeature(ctx, f)
	}

	return nil
}

// ExtendFeatureBudget raises a feature's budget by usd: an extension the
// human granted.
func (s *Service) ExtendFeatureBudget(ctx context.Context, id string, usd float64) (Feature, error) {
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()

	f, err := s.Store.GetFeature(ctx, id)
	if err != nil {
		return Feature{}, err
	}

	if !f.IsSet() {
		return Feature{}, fmt.Errorf("%w: %s has no budget", ErrConflict, f.ID)
	}

	grown := *f.USD + usd
	f.USD = &grown

	return f, s.Store.UpdateFeature(ctx, f)
}
