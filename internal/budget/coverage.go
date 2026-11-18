package budget

import (
	"context"
	"slices"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Assignment is a ticket in progress: what a ticket or feature budget holds.
type Assignment struct {
	Ticket  string
	Feature string
	Agent   string
}

// Covered lists the agents a budget holds once reached: a ticket's
// assignee, the agents working on a feature's tickets, an agent, a team's
// members, or every agent for the staff. The director is never held.
func Covered(scope Scope, o *org.Org, inProgress []Assignment) []string {
	var candidates []string

	switch scope.Kind {
	case KindTicket, KindFeature:
		candidates = workingOn(scope, inProgress)
	case KindAgent:
		candidates = []string{scope.ID}
	case KindTeam, KindStaff:
		for _, m := range o.Members() {
			if scope.Kind == KindStaff || m.Team.ID == scope.ID {
				candidates = append(candidates, m.Address)
			}
		}
	}

	return heldAmong(candidates, o)
}

// workingOn lists the agents working on a ticket, or on a feature's tickets.
func workingOn(scope Scope, inProgress []Assignment) []string {
	var agents []string

	for _, a := range inProgress {
		if scope.Kind == KindTicket && a.Ticket == scope.ID || scope.Kind == KindFeature && a.Feature == scope.ID {
			agents = append(agents, a.Agent)
		}
	}

	return agents
}

// heldAmong keeps the agents of the organization, without the director,
// sorted and once each.
func heldAmong(candidates []string, o *org.Org) []string {
	agents := []string{}

	for _, a := range candidates {
		if _, ok := o.Member(a); ok && !o.IsGod(a) && !slices.Contains(agents, a) {
			agents = append(agents, a)
		}
	}

	slices.Sort(agents)

	return agents
}

// Figures is where the gauges' figures come from (the store).
type Figures interface {
	// SpendTotals is what the staff, agents and teams spent in a month
	// ("YYYY-MM"), or all time for "".
	SpendTotals(ctx context.Context, month string) (Totals, error)
	// BudgetedWork lists the started features and the open or in-progress
	// tickets that have a budget.
	BudgetedWork(ctx context.Context) ([]Budgeted, error)
	// MonthlyExtensions are the extensions of monthly caps granted for a
	// month.
	MonthlyExtensions(ctx context.Context, month string) (map[Scope]float64, error)
}

// Evaluate says where every budget stands now (Gauges), reading the spend
// from src. None while budgets are turned off.
func Evaluate(ctx context.Context, s Settings, src Figures, now time.Time) ([]Gauge, error) {
	if !s.Enabled {
		return []Gauge{}, nil
	}

	month, err := src.SpendTotals(ctx, Month(now))
	if err != nil {
		return nil, err
	}

	allTime, err := src.SpendTotals(ctx, "")
	if err != nil {
		return nil, err
	}

	budgeted, err := src.BudgetedWork(ctx)
	if err != nil {
		return nil, err
	}

	extensions, err := src.MonthlyExtensions(ctx, Month(now))
	if err != nil {
		return nil, err
	}

	return Gauges(s, month, allTime, extensions, budgeted), nil
}
