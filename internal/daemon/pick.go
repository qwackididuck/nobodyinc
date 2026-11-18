package daemon

import (
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// pickMember chooses which member of a team gets something addressed to the
// whole team (a message to "@team", a queued ticket): an idle member first,
// then any running one not on budget hold, then anyone. exclude skips one address (e.g. the
// agent that just released a ticket). Members on leave are never picked. It
// returns "" for an unknown team, or when everyone is on leave.
func pickMember(o *org.Org, statuses *runtime.Statuses, team, exclude string) string {
	var members []string

	for _, m := range o.Members() {
		if m.Team.ID == team && m.Address != exclude && !m.Agent.OnLeave {
			members = append(members, m.Address)
		}
	}

	preferences := []func(runtime.Status) bool{
		func(s runtime.Status) bool { return s == runtime.StatusIdle },
		func(s runtime.Status) bool {
			return s != runtime.StatusStopped && s != runtime.StatusExited && s != runtime.StatusBudgetHold
		},
		func(runtime.Status) bool { return true },
	}
	for _, preferred := range preferences {
		for _, m := range members {
			if preferred(statuses.Get(m)) {
				return m
			}
		}
	}

	return ""
}
