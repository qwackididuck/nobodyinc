package budget

import (
	"maps"
	"slices"
)

// Totals is what the staff, each agent and each team spent over a period.
type Totals struct {
	StaffUSD float64
	Agents   map[string]float64
	Teams    map[string]float64
}

// Budgeted is a ticket or feature with a budget, and what it spent.
type Budgeted struct {
	Scope    Scope
	LimitUSD float64
	SpentUSD float64
	// Threshold is its own threshold; 0 keeps the project's.
	Threshold int
}

// Gauges says where every budget that is set stands: the staff, team and
// agent caps over their own period (month for a monthly cap, raised by its
// extensions of the month; allTime for a cumulative one), then the features
// and tickets given. A gauge at its threshold holds its agents. None while
// budgets are turned off.
func Gauges(s Settings, month, allTime Totals, extensions map[Scope]float64, budgeted []Budgeted) []Gauge {
	gauges := []Gauge{}
	if !s.Enabled {
		return gauges
	}

	capGauge := func(scope Scope, c Cap, spent func(Totals) float64) Gauge {
		period, limit := allTime, c.LimitUSD
		if c.Monthly {
			period, limit = month, limit+extensions[scope]
		}

		return Gauge{
			Scope: scope, LimitUSD: limit, SpentUSD: spent(period),
			Threshold: s.ThresholdOf(c.Threshold), Monthly: c.Monthly,
		}
	}

	if s.Staff != nil {
		gauges = append(gauges, capGauge(Scope{Kind: KindStaff}, *s.Staff, func(t Totals) float64 { return t.StaffUSD }))
	}

	for _, id := range slices.Sorted(maps.Keys(s.Teams)) {
		gauges = append(gauges, capGauge(Scope{Kind: KindTeam, ID: id}, s.Teams[id], func(t Totals) float64 { return t.Teams[id] }))
	}

	for _, address := range slices.Sorted(maps.Keys(s.Agents)) {
		spent := func(t Totals) float64 { return t.Agents[address] }
		gauges = append(gauges, capGauge(Scope{Kind: KindAgent, ID: address}, s.Agents[address], spent))
	}

	for _, b := range budgeted {
		gauges = append(gauges, Gauge{Scope: b.Scope, LimitUSD: b.LimitUSD, SpentUSD: b.SpentUSD, Threshold: s.ThresholdOf(b.Threshold)})
	}

	for i := range gauges {
		gauges[i].Held = gauges[i].Reached()
	}

	return gauges
}

// Reached reports whether a gauge is at or past its threshold.
func (g Gauge) Reached() bool {
	return g.SpentUSD >= g.LimitUSD*float64(g.Threshold)/100
}
