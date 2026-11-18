package budget

// Line is what one agent, team, feature or schedule spent.
type Line struct {
	// Key is the agent's address, the team's or schedule's id, or the
	// feature's id.
	Key string `json:"key"`
	// Label is the feature's or schedule's title.
	Label    string  `json:"label,omitempty"`
	SpentUSD float64 `json:"spent_usd"`
}

// Summary is what was spent in a month (GET /api/spend).
type Summary struct {
	Month string `json:"month"`
	// TotalUSD is everything spent in the month: the staff's spend.
	TotalUSD  float64 `json:"total_usd"`
	Agents    []Line  `json:"agents"`
	Teams     []Line  `json:"teams"`
	Features  []Line  `json:"features"`
	Schedules []Line  `json:"schedules"`
	// Gauges is where every budget that is set stands now.
	Gauges []Gauge `json:"gauges"`
}

// Scope names a budget.
type Scope struct {
	Kind Kind `json:"kind"`
	// ID is the ticket, feature, agent address or team id; "" for the staff.
	ID string `json:"id,omitempty"`
}

// Kind is the level of a budget.
type Kind string

// The five levels of budget (ADR-0013, point 3).
const (
	KindTicket  Kind = "ticket"
	KindFeature Kind = "feature"
	KindAgent   Kind = "agent"
	KindTeam    Kind = "team"
	KindStaff   Kind = "staff"
)

// Gauge is where one budget stands, over its own period.
type Gauge struct {
	Scope    Scope   `json:"scope"`
	LimitUSD float64 `json:"limit_usd"`
	SpentUSD float64 `json:"spent_usd"`
	// Threshold is the percent that applies, the project's or the budget's.
	Threshold int  `json:"threshold"`
	Monthly   bool `json:"monthly,omitempty"`
	// Held is true once the budget reached its threshold and holds agents.
	Held bool `json:"held,omitempty"`
}
