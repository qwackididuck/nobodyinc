// Package budget measures what the agents spend and holds them to their
// budgets (ADR-0013, docs/budgets.md).
//
// # Measuring
//
// Claude Code computes what a session costs, as an estimate at list price in
// USD, and hands it to the session's statusLine command with every update:
// cost.total_cost_usd, the session's running total. Every agent's statusLine
// is `nobody-hook statusline`, which posts that JSON to the daemon. The Meter
// turns the running totals into spend records, each the delta since the
// previous report of the same agent and session:
//
//   - a new session (another session ID) starts from 0;
//   - so does a new process: the supervisor calls Forget when it starts an
//     agent, because a resumed session keeps its ID but its total starts
//     again from 0 (checked by test/contract/statusline_test.go);
//   - within a running session the total only grows: a lower total is a
//     report that arrived late, behind a newer one, and is ignored.
//
// An urgent question's fork (runtime.Forker) is another process, which the
// agent's statusLine never sees: its cost, read from its own output, is
// recorded with Fork and charged to the agent that asked, not to the one
// that answered.
//
// Each record is attributed when it is written, from the daemon's state at
// that moment (Attribute): the agent, its team, its ticket in progress and
// that ticket's feature, and the schedule occurrence it runs. Spend with no
// ticket in progress counts for the agent, its team and the staff only.
package budget

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Source says where a spend record comes from.
type Source string

// The sources of spend.
const (
	// SourceStatusLine: a delta of an agent session's running total.
	SourceStatusLine Source = "statusline"
	// SourceFork: the cost of an urgent question's fork, charged to the asker.
	SourceFork Source = "fork"
)

// Attribution is what a spend record counts for.
type Attribution struct {
	// Agent is the agent that spent it, or that asked a fork; "" for a fork
	// the human asked for, which counts for the staff only.
	Agent string `json:"agent,omitempty"`
	// Team is the agent's team; "" for the director and the human.
	Team string `json:"team,omitempty"`
	// Ticket is the agent's ticket in progress ("T-12"), if any.
	Ticket string `json:"ticket,omitempty"`
	// Feature is that ticket's feature ("F-3"), if any.
	Feature string `json:"feature,omitempty"`
	// ScheduleRun is the schedule occurrence the agent runs, if any.
	ScheduleRun int64 `json:"schedule_run,omitempty"`
}

// Record is one amount spent.
type Record struct {
	Attribution

	ID int64 `json:"id"`
	// At is when it was recorded; Month is its "YYYY-MM" in the machine's
	// time zone, the month it counts for.
	At    time.Time `json:"at"`
	Month string    `json:"month"`
	// Source and Session say where it comes from.
	Source  Source `json:"source"`
	Session string `json:"session,omitempty"`
	// AmountUSD is Claude Code's estimate at list price.
	AmountUSD float64 `json:"amount_usd"`
}

// Store saves spend records.
type Store interface {
	InsertSpend(ctx context.Context, r Record) (Record, error)
}

// Meter turns the running totals the agents' statusLine reports into spend
// records. It is safe for concurrent use.
type Meter struct {
	Store Store
	// Attribute says what an agent's spend counts for right now.
	Attribute func(ctx context.Context, address string) (Attribution, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// OnRecorded, when set, is called after each record is saved.
	OnRecorded func(Record)

	mu sync.Mutex
	// last is the latest running total reported by each agent's session.
	last map[string]sessionTotal
}

// sessionTotal is a session's running total.
type sessionTotal struct {
	session string
	usd     float64
}

// Forget drops what the meter knows of an agent's session: its next report
// starts from 0. The supervisor calls it whenever it starts the agent.
func (m *Meter) Forget(address string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.last, address)
}

// Report records what an agent's session spent since its previous report,
// given the session's running total. Nothing is recorded when the session
// spent nothing new.
func (m *Meter) Report(ctx context.Context, address, session string, totalUSD float64) error {
	if totalUSD < 0 {
		return fmt.Errorf("negative running total %v", totalUSD)
	}

	m.mu.Lock()
	defer m.mu.Unlock() // held while saving, so that records keep their order

	previous, known := m.last[address]

	delta := Delta(previous, known, session, totalUSD)
	if delta <= 0 {
		if !known || previous.session != session {
			m.remember(address, sessionTotal{session: session, usd: totalUSD})
		}

		return nil
	}

	attribution, err := m.Attribute(ctx, address)
	if err != nil {
		return fmt.Errorf("attribute the spend of %s: %w", address, err)
	}

	if err := m.save(ctx, Record{Source: SourceStatusLine, Session: session, AmountUSD: delta, Attribution: attribution}); err != nil {
		return err
	}

	m.remember(address, sessionTotal{session: session, usd: totalUSD})

	return nil
}

// Fork records the cost of an urgent question's fork, charged to the asker:
// an agent's ticket in progress, or the agent itself, its team and the
// staff. An empty asker is the human: the cost counts for the staff only.
func (m *Meter) Fork(ctx context.Context, asker, session string, costUSD float64) error {
	if costUSD <= 0 {
		return nil
	}

	var attribution Attribution

	if asker != "" {
		var err error
		if attribution, err = m.Attribute(ctx, asker); err != nil {
			return fmt.Errorf("attribute the fork asked by %s: %w", asker, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.save(ctx, Record{Source: SourceFork, Session: session, AmountUSD: costUSD, Attribution: attribution})
}

// Delta is what a session spent since the previous report, given the
// previous report (known is false when there is none) and the new running
// total. A new session starts from 0; within a session, a total lower than
// the previous one is a late report, worth nothing.
func Delta(previous sessionTotal, known bool, session string, totalUSD float64) float64 {
	if !known || previous.session != session {
		return totalUSD
	}

	if totalUSD <= previous.usd {
		return 0
	}

	return totalUSD - previous.usd
}

// Month is the month a moment counts for, "YYYY-MM" in the machine's time
// zone (as schedules read times, ADR-0012).
func Month(t time.Time) string {
	return t.In(time.Local).Format("2006-01")
}

func (m *Meter) remember(address string, t sessionTotal) {
	if m.last == nil {
		m.last = map[string]sessionTotal{}
	}

	m.last[address] = t
}

// save stamps and saves a record; m.mu must be held.
func (m *Meter) save(ctx context.Context, r Record) error {
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}

	r.At = now().UTC()
	r.Month = Month(r.At)

	saved, err := m.Store.InsertSpend(ctx, r)
	if err != nil {
		return fmt.Errorf("record spend: %w", err)
	}

	if m.OnRecorded != nil {
		m.OnRecorded(saved)
	}

	return nil
}
