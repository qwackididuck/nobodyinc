package subagents

import (
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Status says where a subagent stands with the organization (ADR-0018).
type Status string

// The statuses.
const (
	// StatusPending: neither imported nor dismissed; offered to the human.
	StatusPending Status = "pending"
	// StatusImported: an agent has from: subagent:<name>.
	StatusImported Status = "imported"
	// StatusIgnored: its name is in org.yaml's ignored_subagents.
	StatusIgnored Status = "ignored"
)

// StatusIn returns the status of the subagent name in the organization, and
// the address of the agent imported from it ("" unless imported). An
// imported subagent counts as imported even if it was also dismissed.
func StatusIn(o *org.Org, name string) (Status, string) {
	for _, m := range o.Members() {
		if m.Agent.From == Origin(name) {
			return StatusImported, m.Address
		}
	}

	if slices.Contains(o.IgnoredSubagents, name) {
		return StatusIgnored, ""
	}

	return StatusPending, ""
}

// Pending returns the subagents offered to the human: readable, neither
// imported nor dismissed.
func Pending(o *org.Org, all []Subagent) []Subagent {
	var out []Subagent

	for _, s := range all {
		if s.Err != "" {
			continue
		}

		if status, _ := StatusIn(o, s.Name); status == StatusPending {
			out = append(out, s)
		}
	}

	return out
}
