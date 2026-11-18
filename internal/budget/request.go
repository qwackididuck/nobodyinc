package budget

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Request is the one extension request of a budget at its threshold
// (ADR-0013, point 5). It stays active while its budget is reached: an
// extension, a monthly reset, a budget raised or removed, or budgets turned
// off end it. A budget reached again later gets a new request.
type Request struct {
	ID     string        `json:"id"` // "BR-4"
	Scope  Scope         `json:"scope"`
	Status RequestStatus `json:"status"`
	// Active is true while its budget is reached; at most one request per
	// budget is active.
	Active bool `json:"active"`
	// HeldAgents are the agents its budget holds now (computed, not stored).
	HeldAgents []string `json:"held_agents"`
	// AskedAgent is the agent asked to justify it, the first to stop.
	AskedAgent string `json:"asked_agent,omitempty"`
	// RequestedBy is the agent that justified it, or the director for a
	// staff or feature budget.
	RequestedBy   string   `json:"requested_by,omitempty"`
	AmountUSD     *float64 `json:"amount_usd,omitempty"`
	Justification string   `json:"justification,omitempty"`
	// DirectorOpinion and RecommendedUSD are what the director adds when it
	// passes the request on to the human.
	DirectorOpinion string   `json:"director_opinion,omitempty"`
	RecommendedUSD  *float64 `json:"recommended_usd,omitempty"`
	// LimitUSD and SpentUSD are where the budget stood when it was reached.
	LimitUSD float64 `json:"limit_usd"`
	SpentUSD float64 `json:"spent_usd"`
	// CardMessage is the director's message to the human carrying the card.
	CardMessage  string     `json:"card_message,omitempty"`
	ExtendedUSD  *float64   `json:"extended_usd,omitempty"`
	Refusal      *Refusal   `json:"refusal,omitempty"`
	ClosedReason string     `json:"closed_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	SubmittedAt  *time.Time `json:"submitted_at,omitempty"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
}

// RequestStatus is where a request stands.
type RequestStatus string

// The life of a request: the first agent to stop is asked to justify it
// (ticket, agent and team budgets) or the director is (staff and feature
// budgets); the director passes it on to the human with its opinion; the
// human extends or refuses. A request whose budget is no longer reached
// before the human decides is closed.
const (
	RequestAwaitingAgent    RequestStatus = "awaiting_agent"
	RequestAwaitingDirector RequestStatus = "awaiting_director"
	RequestAwaitingHuman    RequestStatus = "awaiting_human"
	RequestExtended         RequestStatus = "extended"
	RequestRefused          RequestStatus = "refused"
	RequestClosed           RequestStatus = "closed"
)

// Open reports whether the request still waits for someone.
func (s RequestStatus) Open() bool {
	return s == RequestAwaitingAgent || s == RequestAwaitingDirector || s == RequestAwaitingHuman
}

// Refusal is the human's answer when refusing an extension.
type Refusal struct {
	// Outcomes says what happens to each held agent, by address.
	Outcomes map[string]Outcome `json:"outcomes"`
	// CommitFirst releases each held agent with a ticket for one short turn
	// to commit its work in progress before its outcome applies.
	CommitFirst bool   `json:"commit_first,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// Outcome is what happens to a held agent when an extension is refused.
type Outcome string

// The outcomes the human chooses from.
const (
	OutcomeCancel  Outcome = "cancel"  // cancel its ticket
	OutcomeRelease Outcome = "release" // give its ticket back to its team's queue
	OutcomeHold    Outcome = "hold"    // stay on budget hold
)

// Global reports whether a budget is the director's to justify, since it
// has the overall view: the staff's and a feature's (ADR-0013, point 5).
func (s Scope) Global() bool {
	return s.Kind == KindStaff || s.Kind == KindFeature
}

// String names a budget for people: "ticket T-12", "team dev", "the staff".
func (s Scope) String() string {
	switch s.Kind {
	case KindStaff:
		return "the staff"
	case KindAgent, KindTicket, KindFeature, KindTeam:
		return string(s.Kind) + " " + s.ID
	default:
		return string(s.Kind) + " " + s.ID
	}
}

// FormatRequestID is a request's ID: "BR-<n>".
func FormatRequestID(n int64) string {
	return "BR-" + strconv.FormatInt(n, 10)
}

// ParseRequestID reads a request's ID.
func ParseRequestID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "BR-"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "BR-") || n < 1 {
		return 0, fmt.Errorf("%w: budget request %q", ErrNotFound, id)
	}

	return n, nil
}
