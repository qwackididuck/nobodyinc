// Package work holds the tickets: the units of work the organization tracks.
//
// A ticket belongs to a team. While it has no assignee it waits in that team's
// queue; any member can claim it, provided every ticket it depends on is done.
// The assignee then reports progress with comments and deliverables, and
// closes it.
//
//	open ──claim──► in_progress ──done──► done
//	  ▲                 │
//	  └────release──────┘        (any open state) ──cancel──► canceled
//
// Who may do what (see Service):
//   - the human and the system may do anything;
//   - anyone may create a ticket for any team: a team's block list only
//     limits who its members start conversations with (ADR-0009);
//   - only a member of the owning team can claim a ticket;
//   - only the assignee (or the human) can release, finish or cancel it;
//   - anyone in the organization can comment or attach a deliverable.
package work

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Status is where a ticket stands.
type Status string

// The states of a ticket.
const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusCanceled   Status = "canceled"
)

// Closed reports whether no more work is expected on the ticket.
func (s Status) Closed() bool {
	return s == StatusDone || s == StatusCanceled
}

// Ticket is a unit of work owned by a team.
type Ticket struct {
	// Budget is optional; inside a feature with a budget, every ticket has
	// one (ADR-0013, point 3).
	Budget

	ID                 string    `json:"id"` // "T-12"
	Title              string    `json:"title"`
	Description        string    `json:"description,omitempty"`
	AcceptanceCriteria string    `json:"acceptance_criteria,omitempty"`
	Team               string    `json:"team"`
	Feature            string    `json:"feature,omitempty"` // "F-3", the feature it belongs to
	Assignee           string    `json:"assignee,omitempty"`
	Status             Status    `json:"status"`
	BlockedBy          []string  `json:"blocked_by,omitempty"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	// Archived: the ticket of a finished feature, kept for the record and no
	// longer worked on (ADR-0007).
	Archived bool `json:"archived,omitempty"`
	// SpentUSD is what was spent on it, all time (ADR-0013); read-only,
	// computed by the store.
	SpentUSD float64 `json:"spent_usd"`
}

// Deliverable is something produced for a ticket.
type Deliverable struct {
	ID        int64     `json:"id"`
	Ticket    string    `json:"ticket"`
	Author    string    `json:"author"`
	Kind      string    `json:"kind"`                // file, commit, report or note
	Reference string    `json:"reference,omitempty"` // path, commit hash, URL…
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// Comment is a remark on a ticket.
type Comment struct {
	ID        int64     `json:"id"`
	Ticket    string    `json:"ticket"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// DeliverableKinds are the accepted kinds of deliverables.
var DeliverableKinds = []string{"file", "commit", "report", "note"}

// FormatID turns a stored ticket number into its ID: 12 → "T-12".
func FormatID(n int64) string {
	return "T-" + strconv.FormatInt(n, 10)
}

// ParseID turns "T-12" (or "12") into 12.
func ParseID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(id)), "T-"), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: %q (expected T-<number>)", ErrInvalid, id)
	}

	return n, nil
}
