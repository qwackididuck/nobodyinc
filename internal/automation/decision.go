package automation

import (
	"errors"
	"time"
)

// Impact says how much a decision taken in the human's place matters, so that
// the human reads the heavy ones first when they come back.
type Impact string

// The impacts the director chooses from. High is for what is hard to undo,
// costs money, widens the scope or touches security.
const (
	ImpactLow    Impact = "low"
	ImpactMedium Impact = "medium"
	ImpactHigh   Impact = "high"
)

// Valid reports whether i is one of the impacts.
func (i Impact) Valid() bool { return i == ImpactLow || i == ImpactMedium || i == ImpactHigh }

// Decision is an answer the director gave to an agent's question in the
// human's place (Nobody level), kept so that the human can review it.
type Decision struct {
	ID int64 `json:"id"`
	// AskedBy is the agent that asked.
	AskedBy string `json:"asked_by"`
	// Message is the question's message ID.
	Message string `json:"message"`
	// Question is the question's subject.
	Question  string    `json:"question"`
	Decision  string    `json:"decision"`
	Rationale string    `json:"rationale"`
	Impact    Impact    `json:"impact"`
	DecidedAt time.Time `json:"decided_at"`
	// Seen is true once the human acknowledged the decision.
	Seen bool `json:"seen"`
}

// ErrDecided is returned when a question already has a decision.
var ErrDecided = errors.New("this question already has a decision")
