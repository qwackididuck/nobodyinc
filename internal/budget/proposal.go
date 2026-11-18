package budget

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Proposal is an agent, team or staff cap the director proposes; only the
// human's acceptance writes it to budgets.yaml (ADR-0013, point 8).
type Proposal struct {
	ID    string `json:"id"` // "BP-1"
	Scope Scope  `json:"scope"`
	Cap   Cap    `json:"cap"`
	// Reason is why the director proposes it.
	Reason          string         `json:"reason,omitempty"`
	Status          ProposalStatus `json:"status"`
	ProposedBy      string         `json:"proposed_by"`
	ProposalMessage string         `json:"proposal_message,omitempty"`
	RefusalReason   string         `json:"refusal_reason,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	DecidedAt       *time.Time     `json:"decided_at,omitempty"`
}

// ProposalStatus is where a proposal stands.
type ProposalStatus string

// Proposal statuses.
const (
	ProposalPending  ProposalStatus = "proposed"
	ProposalAccepted ProposalStatus = "accepted"
	ProposalRefused  ProposalStatus = "refused"
)

// IsCap reports whether a scope is one of budgets.yaml's: an agent, a team
// or the staff.
func (s Scope) IsCap() bool {
	return s.Kind == KindAgent || s.Kind == KindTeam || s.Kind == KindStaff
}

// FormatProposalID is a proposal's ID: "BP-<n>".
func FormatProposalID(n int64) string {
	return "BP-" + strconv.FormatInt(n, 10)
}

// ParseProposalID reads a proposal's ID.
func ParseProposalID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "BP-"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "BP-") || n < 1 {
		return 0, fmt.Errorf("%w: budget proposal %q", ErrNotFound, id)
	}

	return n, nil
}
