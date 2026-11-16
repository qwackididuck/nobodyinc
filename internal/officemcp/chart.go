package officemcp

import (
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// What an agent may do with a team (TeamChart.Reach).
const (
	// ReachOwnTeam: the agent's own team; it talks to them freely.
	ReachOwnTeam = "own_team"
	// ReachChannel: a channel lets the agent start conversations with them.
	ReachChannel = "channel"
	// ReachAnswerOnly: the agent can only answer their messages; to reach
	// them otherwise, it goes through the human.
	ReachAnswerOnly = "answer_only"
	// ReachDirector: the director's view of every team; it writes to anyone.
	ReachDirector = "director"
)

// TeamChart is one team of the organization, as one agent sees it.
type TeamChart struct {
	Team    string        `json:"team"`
	Goal    string        `json:"goal,omitempty"`
	Reach   string        `json:"reach"`
	Members []ChartMember `json:"members"`
}

// ChartMember is a member of a team in the organization chart.
type ChartMember struct {
	Address string `json:"address"`
	Role    string `json:"role,omitempty"`
	// Expertise: what to ask them about, and what they decide.
	Expertise string `json:"expertise,omitempty"`
	// OnLeave: mail to them waits until they return; write to their team
	// instead if it cannot wait.
	OnLeave bool `json:"on_leave,omitempty"`
}

// Chart is the whole organization as seen by the agent at address: its own
// team first, then the teams it can start conversations with, then the
// others. The agent itself is left out.
//
// It goes into the agent's system prompt when it starts, and list_contacts
// returns it up to date: the organization changes while agents run.
func Chart(o *org.Org, address string) []TeamChart {
	myTeam := org.TeamOf(address)

	var own, reachable, others []TeamChart

	for _, t := range o.Teams {
		chart := TeamChart{Team: t.ID, Goal: t.Goal, Members: []ChartMember{}}

		for _, a := range t.Agents {
			if member := org.Address(a.Name, t.ID); member != address {
				chart.Members = append(chart.Members, ChartMember{
					Address: member, Role: a.Role, Expertise: a.Expertise, OnLeave: a.OnLeave,
				})
			}
		}

		switch {
		case o.IsGod(address):
			chart.Reach = ReachDirector
			reachable = append(reachable, chart)
		case t.ID == myTeam:
			chart.Reach = ReachOwnTeam
			own = append(own, chart)
		case o.HasChannel(myTeam, t.ID):
			chart.Reach = ReachChannel
			reachable = append(reachable, chart)
		default:
			chart.Reach = ReachAnswerOnly
			others = append(others, chart)
		}
	}

	return append(append(own, reachable...), others...)
}

// DirectorOf is the director as shown in the organization chart, or nil when
// the project has none.
func DirectorOf(o *org.Org) *ChartMember {
	if o.God == nil {
		return nil
	}

	return &ChartMember{Address: o.God.Name, Role: o.God.Role, Expertise: o.God.Expertise}
}
