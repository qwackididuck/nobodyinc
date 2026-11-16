package daemon

import (
	"context"
	"log/slog"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Org-change notices. An agent's system prompt holds the organization as it
// was when its session started; when it changes (an arrival, a departure, a
// leave, a new team...), the other agents get one inform notice from nobody
// summing it up, so that they know whom to ask (docs/agents.md, "Whom to
// ask"). The agent the change is about is not told about itself.

// orgChange is one change of the organization, as told to the agents.
type orgChange struct {
	// About is the address of the agent the change is about, who is not told;
	// "" when it is about no agent (a team).
	About string
	Text  string
}

// orgChanges lists what changed between two snapshots of the organization:
// arrivals, departures, leaves and returns, new roles or expertise, and new
// teams or goals. It is pure: the order follows the organization.
func orgChanges(before, after *org.Org) []orgChange {
	var changes []orgChange

	for _, t := range after.Teams {
		old, existed := before.Team(t.ID)

		switch {
		case !existed:
			changes = append(changes, orgChange{Text: "New team " + t.ID + withDetail(": ", t.Goal) + "."})
		case old.Goal != t.Goal:
			changes = append(changes, orgChange{Text: "Team " + t.ID + " has a new goal" + withDetail(": ", t.Goal) + "."})
		}
	}

	for _, m := range after.Members() {
		old, existed := before.Member(m.Address)
		if text := memberChange(old, m, existed); text != "" {
			changes = append(changes, orgChange{About: m.Address, Text: text})
		}
	}

	for _, m := range before.Members() {
		if _, stays := after.Member(m.Address); !stays {
			changes = append(changes, orgChange{About: m.Address, Text: m.Address + withRole(m.Agent.Role) + " left the organization."})
		}
	}

	return changes
}

// memberChange describes what changed for one agent still (or newly) in the
// organization; "" when nothing others need to know changed.
func memberChange(old, m org.Member, existed bool) string {
	a := m.Agent

	switch {
	case !existed && m.Team.ID == "":
		return m.Address + withRole(a.Role) + " is now the director." + askAbout(a.Expertise)
	case !existed:
		return m.Address + withRole(a.Role) + " joined team " + m.Team.ID + "." + askAbout(a.Expertise)
	case !old.Agent.OnLeave && a.OnLeave:
		return m.Address + " is on leave: mail to them waits until they return; write to their team if it cannot wait."
	case old.Agent.OnLeave && !a.OnLeave:
		return m.Address + " is back from leave."
	case old.Agent.Role != a.Role || old.Agent.Expertise != a.Expertise:
		return m.Address + " is now " + orUnnamed(a.Role) + "." + askAbout(a.Expertise)
	}

	return ""
}

// orgNotices sends each agent the changes that are not about itself.
type orgNotices struct {
	messages *messaging.Service
	logger   *slog.Logger
}

func (n *orgNotices) send(ctx context.Context, before, after *org.Org) {
	changes := orgChanges(before, after)
	if len(changes) == 0 {
		return
	}

	for _, m := range after.Members() {
		var lines []string

		for _, c := range changes {
			if c.About != m.Address {
				lines = append(lines, "- "+c.Text)
			}
		}

		if len(lines) == 0 {
			continue
		}

		_, err := n.messages.Send(ctx, messaging.Draft{
			From: org.SystemAddress, To: m.Address, Act: messaging.ActInform, Priority: messaging.Normal,
			Subject: "The organization changed",
			Body: "For your information, so that you know whom to ask (`list_contacts` shows the whole organization):\n" +
				strings.Join(lines, "\n"),
		})
		if err != nil {
			n.logger.Error("tell an agent the organization changed", "agent", m.Address, "err", err)
		}
	}
}

func withRole(role string) string {
	return withDetail(" (", role) + closeIf(role, ")")
}

func askAbout(expertise string) string {
	return withDetail(" Ask them about: ", expertise) + closeIf(expertise, ".")
}

func withDetail(prefix, s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}

	return prefix + strings.TrimSpace(s)
}

func closeIf(s, suffix string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}

	return suffix
}

func orUnnamed(role string) string {
	if strings.TrimSpace(role) == "" {
		return "without a role"
	}

	return role
}
