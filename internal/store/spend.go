package store

import (
	"context"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/budget"
)

// InsertSpend stores a spend record and returns it with its ID (ADR-0013).
func (s *Store) InsertSpend(ctx context.Context, r budget.Record) (budget.Record, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO spend (at, month, source, session, agent, team, ticket, feature, schedule_run, amount_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(r.At), r.Month, r.Source, r.Session, r.Agent, r.Team, r.Ticket, r.Feature, r.ScheduleRun, r.AmountUSD)
	if err != nil {
		return budget.Record{}, fmt.Errorf("insert spend: %w", err)
	}

	if r.ID, err = res.LastInsertId(); err != nil {
		return budget.Record{}, err
	}

	return r, nil
}

// SpendSummary returns what was spent in a month ("YYYY-MM"): the total, and
// the spend per agent, team, feature and schedule, largest first. Gauges are
// left empty: they depend on the budgets, not only on the spend.
func (s *Store) SpendSummary(ctx context.Context, month string) (budget.Summary, error) {
	sum := budget.Summary{Month: month, Gauges: []budget.Gauge{}}

	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_usd), 0) FROM spend WHERE month = ?`, month).
		Scan(&sum.TotalUSD); err != nil {
		return budget.Summary{}, fmt.Errorf("total spend of %s: %w", month, err)
	}

	queries := []struct {
		into  *[]budget.Line
		query string
	}{
		{&sum.Agents, `
			SELECT agent, '', SUM(amount_usd) FROM spend
			WHERE month = ? AND agent != '' GROUP BY agent ORDER BY 3 DESC, 1`},
		{&sum.Teams, `
			SELECT team, '', SUM(amount_usd) FROM spend
			WHERE month = ? AND team != '' GROUP BY team ORDER BY 3 DESC, 1`},
		{&sum.Features, `
			SELECT spend.feature, COALESCE(features.title, ''), SUM(spend.amount_usd) FROM spend
			LEFT JOIN features ON 'F-' || features.id = spend.feature
			WHERE spend.month = ? AND spend.feature != '' GROUP BY spend.feature ORDER BY 3 DESC, 1`},
		// A schedule's title is the one of its latest occurrence that spent.
		{&sum.Schedules, `
			SELECT schedule_runs.schedule_id, MAX(schedule_runs.schedule_title), SUM(spend.amount_usd) FROM spend
			JOIN schedule_runs ON schedule_runs.id = spend.schedule_run
			WHERE spend.month = ? GROUP BY schedule_runs.schedule_id ORDER BY 3 DESC, 1`},
	}

	for _, q := range queries {
		lines, err := s.spendLines(ctx, q.query, month)
		if err != nil {
			return budget.Summary{}, err
		}

		*q.into = lines
	}

	return sum, nil
}

func (s *Store) spendLines(ctx context.Context, query, month string) ([]budget.Line, error) {
	rows, err := s.db.QueryContext(ctx, query, month)
	if err != nil {
		return nil, fmt.Errorf("spend of %s: %w", month, err)
	}
	defer rows.Close()

	lines := []budget.Line{}

	for rows.Next() {
		var l budget.Line
		if err := rows.Scan(&l.Key, &l.Label, &l.SpentUSD); err != nil {
			return nil, fmt.Errorf("read spend: %w", err)
		}

		lines = append(lines, l)
	}

	return lines, rows.Err()
}

// The spend of a ticket, a feature and a schedule occurrence, all time,
// added to their SELECT (selectTickets, selectFeatures, selectRuns).
const (
	ticketSpent  = `(SELECT COALESCE(SUM(amount_usd), 0) FROM spend WHERE spend.ticket = 'T-' || tickets.id)`
	featureSpent = `(SELECT COALESCE(SUM(amount_usd), 0) FROM spend WHERE spend.feature = 'F-' || features.id)`
	runSpent     = `(SELECT COALESCE(SUM(amount_usd), 0) FROM spend WHERE spend.schedule_run = schedule_runs.id)`
)
