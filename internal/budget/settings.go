package budget

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// DefaultThreshold is the percent of a budget at which its agents stop when
// the project sets none (ADR-0013, point 4).
const DefaultThreshold = 80

// ErrInvalid is returned for settings or a budget that cannot be used.
var ErrInvalid = errors.New("invalid budget")

// Settings are the project's budget settings and caps, the content of
// .nobody/budgets.yaml (ADR-0013, point 3). Ticket and feature budgets live
// with the tickets and features, in the database.
type Settings struct {
	// Enabled false turns caps, thresholds, holds, requests and budget
	// fields off; spend is still measured.
	Enabled bool `json:"enabled"`
	// Threshold is the project's percent at which a budget stops its agents.
	Threshold int `json:"threshold"`
	// Staff caps every agent together, the director included.
	Staff *Cap `json:"staff,omitempty"`
	// Teams and Agents cap a team, by id, or an agent, by address. The
	// director has no cap of its own: it is never held for budget.
	Teams  map[string]Cap `json:"teams,omitempty"`
	Agents map[string]Cap `json:"agents,omitempty"`
}

// Cap is an agent, team or staff cap.
type Cap struct {
	LimitUSD float64 `json:"limit_usd" yaml:"limit_usd"`
	// Monthly caps start again from 0 on the first day of each month, in
	// the machine's time zone; others are cumulative.
	Monthly bool `json:"monthly,omitempty" yaml:"monthly,omitempty"`
	// Threshold overrides the project's; 0 keeps it.
	Threshold int `json:"threshold,omitempty" yaml:"threshold,omitempty"`
}

// Defaults are the settings of a project without budgets.yaml: budgets on,
// no caps, the default threshold.
func Defaults() Settings {
	return Settings{Enabled: true, Threshold: DefaultThreshold}
}

// ThresholdOf is the threshold that applies to a budget whose own threshold
// is own (0: none).
func (s Settings) ThresholdOf(own int) int {
	if own > 0 {
		return own
	}

	return s.Threshold
}

// Check checks the settings on their own: thresholds and amounts.
func (s Settings) Check() error {
	var errs []error

	if err := checkThreshold("the project's threshold", s.Threshold); err != nil {
		errs = append(errs, err)
	}

	if s.Staff != nil {
		errs = append(errs, s.Staff.check("the staff"))
	}

	for _, id := range slices.Sorted(maps.Keys(s.Teams)) {
		errs = append(errs, s.Teams[id].check("team "+id))
	}

	for _, address := range slices.Sorted(maps.Keys(s.Agents)) {
		errs = append(errs, s.Agents[address].check("agent "+address))
	}

	return errors.Join(errs...)
}

// CheckOrg checks that every capped team and agent exists, and that the
// director has no cap.
func (s Settings) CheckOrg(o *org.Org) error {
	var errs []error

	for _, id := range slices.Sorted(maps.Keys(s.Teams)) {
		if _, ok := o.Team(id); !ok {
			errs = append(errs, fmt.Errorf("%w: no team %q", ErrInvalid, id))
		}
	}

	for _, address := range slices.Sorted(maps.Keys(s.Agents)) {
		switch _, ok := o.Member(address); {
		case o.IsGod(address):
			errs = append(errs, fmt.Errorf("%w: the director has no cap of its own, it is never held for budget", ErrInvalid))
		case !ok:
			errs = append(errs, fmt.Errorf("%w: no agent %q", ErrInvalid, address))
		}
	}

	return errors.Join(errs...)
}

// Pruned returns the settings without the caps of teams and agents that no
// longer exist, nor a cap on the director.
func (s Settings) Pruned(o *org.Org) Settings {
	s.Teams = maps.Clone(s.Teams)
	maps.DeleteFunc(s.Teams, func(id string, _ Cap) bool {
		_, ok := o.Team(id)

		return !ok
	})

	s.Agents = maps.Clone(s.Agents)
	maps.DeleteFunc(s.Agents, func(address string, _ Cap) bool {
		_, ok := o.Member(address)

		return !ok || o.IsGod(address)
	})

	return s
}

func (c Cap) check(what string) error {
	if c.LimitUSD <= 0 {
		return fmt.Errorf("%w: %s: the limit must be more than 0", ErrInvalid, what)
	}

	if c.Threshold != 0 {
		return checkThreshold(what+"'s threshold", c.Threshold)
	}

	return nil
}

func checkThreshold(what string, percent int) error {
	if percent < 1 || percent > 100 {
		return fmt.Errorf("%w: %s must be between 1 and 100, not %d", ErrInvalid, what, percent)
	}

	return nil
}
