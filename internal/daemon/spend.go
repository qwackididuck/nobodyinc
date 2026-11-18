package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// spendEventInterval is the most often spend.changed is published: the
// agents' statusLine reports several times per turn.
const spendEventInterval = time.Second

// wireSpend builds the meter that records what the agents spend (ADR-0013)
// and plugs it into the supervisor, which forgets a session's running total
// whenever it starts an agent, and into the forker, which charges a fork's
// cost to the asker.
func wireSpend(st *store.Store, opts Options, bus *events.Bus, supervisor *runtime.Supervisor, forker *runtime.Forker) *budget.Meter {
	clock := opts.Clock
	if clock == nil {
		clock = schedule.RealClock{}
	}

	announce := throttle(spendEventInterval, func() {
		bus.Publish(events.Event{Type: events.SpendChanged, Data: events.Empty{}})
	})

	meter := &budget.Meter{
		Store:      st,
		Attribute:  spendAttribution(st),
		Now:        clock.Now,
		OnRecorded: func(budget.Record) { announce() },
	}

	supervisor.OnStart = meter.Forget

	forker.RecordCost = func(ctx context.Context, asker, session string, costUSD float64) {
		if asker == org.HumanAddress {
			asker = "" // the staff only
		}

		if err := meter.Fork(ctx, asker, session, costUSD); err != nil {
			opts.Logger.Error("record the cost of a fork", "asker", asker, "err", err)
		}
	}

	return meter
}

// spendAttribution says what an agent's spend counts for right now: the
// agent, its team, its ticket in progress and that ticket's feature, and
// the schedule occurrence it runs.
func spendAttribution(st *store.Store) func(ctx context.Context, address string) (budget.Attribution, error) {
	return func(ctx context.Context, address string) (budget.Attribution, error) {
		a := budget.Attribution{Agent: address, Team: org.TeamOf(address)}

		tickets, err := st.ListTickets(ctx, work.Filter{Assignee: address, Status: work.StatusInProgress})
		if err != nil {
			return budget.Attribution{}, err
		}

		if len(tickets) > 0 { // an agent works on one ticket at a time
			a.Ticket, a.Feature = tickets[0].ID, tickets[0].Feature
		}

		runs, err := st.ListRuns(ctx, store.RunFilter{Statuses: []schedule.RunStatus{schedule.RunRunning}})
		if err != nil {
			return budget.Attribution{}, err
		}

		for _, r := range runs {
			if r.Agent == address {
				a.ScheduleRun = r.ID

				break
			}
		}

		return a, nil
	}
}

// throttle returns a function that calls fn at most once per interval: the
// first call schedules fn after interval, and the calls until then are
// folded into it.
func throttle(interval time.Duration, fn func()) func() {
	var (
		mu      sync.Mutex
		pending bool
	)

	return func() {
		mu.Lock()
		defer mu.Unlock()

		if pending {
			return
		}

		pending = true

		time.AfterFunc(interval, func() {
			mu.Lock()
			pending = false
			mu.Unlock()

			fn()
		})
	}
}
