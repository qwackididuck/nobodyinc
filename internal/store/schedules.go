package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// RunFilter narrows ListRuns; empty fields match everything.
type RunFilter struct {
	Schedule string
	Statuses []schedule.RunStatus
	// Limit caps the number of runs (newest first); 0 means no limit.
	Limit int
}

// InsertRun stores a new occurrence and returns it with its ID.
func (s *Store) InsertRun(ctx context.Context, r schedule.Run) (schedule.Run, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO schedule_runs (schedule_id, schedule_title, trigger, mode, status, due_at, started_at, ended_at, agent, ticket, message, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ScheduleID, r.ScheduleTitle, r.Trigger, r.Mode, r.Status, formatTime(r.DueAt),
		formatOptionalTime(r.StartedAt), formatOptionalTime(r.EndedAt), r.Agent, r.Ticket, r.Message, r.Note)
	if err != nil {
		return schedule.Run{}, fmt.Errorf("insert schedule run: %w", err)
	}

	if r.ID, err = res.LastInsertId(); err != nil {
		return schedule.Run{}, err
	}

	return r, nil
}

// UpdateRun saves an occurrence's progress: status, times, agent, ticket,
// message and note.
func (s *Store) UpdateRun(ctx context.Context, r schedule.Run) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE schedule_runs SET status = ?, started_at = ?, ended_at = ?, agent = ?, ticket = ?, message = ?, note = ?
		WHERE id = ?`,
		r.Status, formatOptionalTime(r.StartedAt), formatOptionalTime(r.EndedAt), r.Agent, r.Ticket, r.Message, r.Note, r.ID)
	if err != nil {
		return fmt.Errorf("update schedule run %d: %w", r.ID, err)
	}

	return nil
}

// ListRuns returns occurrences, newest first.
func (s *Store) ListRuns(ctx context.Context, f RunFilter) ([]schedule.Run, error) {
	var (
		where []string
		args  []any
	)

	if f.Schedule != "" {
		where, args = append(where, "schedule_id = ?"), append(args, f.Schedule)
	}

	if len(f.Statuses) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")
		where = append(where, "status IN ("+marks+")")

		for _, st := range f.Statuses {
			args = append(args, st)
		}
	}

	query := selectRuns
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	query += " ORDER BY id DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedule runs: %w", err)
	}
	defer rows.Close()

	var runs []schedule.Run

	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}

		runs = append(runs, r)
	}

	return runs, rows.Err()
}

const selectRuns = `
	SELECT id, schedule_id, schedule_title, trigger, mode, status, due_at, started_at, ended_at, agent, ticket, message, note,
		` + runSpent + `
	FROM schedule_runs`

func scanRun(row scanner) (schedule.Run, error) {
	var (
		r                       schedule.Run
		dueAt, started, endedAt string
	)
	if err := row.Scan(&r.ID, &r.ScheduleID, &r.ScheduleTitle, &r.Trigger, &r.Mode, &r.Status, &dueAt, &started, &endedAt,
		&r.Agent, &r.Ticket, &r.Message, &r.Note, &r.SpentUSD); err != nil {
		return schedule.Run{}, fmt.Errorf("read schedule run: %w", err)
	}

	var err error
	if r.DueAt, err = parseTime(dueAt); err != nil {
		return schedule.Run{}, err
	}

	if r.StartedAt, err = parseOptionalTime(started); err != nil {
		return schedule.Run{}, err
	}

	r.EndedAt, err = parseOptionalTime(endedAt)

	return r, err
}

// InsertProposal stores a schedule the director proposed and returns it with
// its ID.
func (s *Store) InsertProposal(ctx context.Context, p schedule.Proposal) (schedule.Proposal, error) {
	spec, err := json.Marshal(p.Schedule)
	if err != nil {
		return schedule.Proposal{}, err
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO schedule_proposals (schedule, status, proposed_by, proposal_message, refusal_reason, created_at, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(spec), p.Status, p.ProposedBy, p.ProposalMessage, p.RefusalReason, formatTime(p.CreatedAt), formatOptionalTime(p.DecidedAt))
	if err != nil {
		return schedule.Proposal{}, fmt.Errorf("insert schedule proposal: %w", err)
	}

	n, err := res.LastInsertId()
	if err != nil {
		return schedule.Proposal{}, err
	}

	p.ID = schedule.FormatProposalID(n)

	return p, nil
}

// GetProposal returns one proposal.
func (s *Store) GetProposal(ctx context.Context, id string) (schedule.Proposal, error) {
	n, err := schedule.ParseProposalID(id)
	if err != nil {
		return schedule.Proposal{}, err
	}

	p, err := scanProposal(s.db.QueryRowContext(ctx, selectProposals+` WHERE id = ?`, n))
	if errors.Is(err, sql.ErrNoRows) {
		return schedule.Proposal{}, fmt.Errorf("%w: proposal %s", schedule.ErrNotFound, id)
	}

	return p, err
}

// UpdateProposal saves a proposal's schedule (edited by the human), status,
// message, refusal reason and decision time.
func (s *Store) UpdateProposal(ctx context.Context, p schedule.Proposal) error {
	n, err := schedule.ParseProposalID(p.ID)
	if err != nil {
		return err
	}

	spec, err := json.Marshal(p.Schedule)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE schedule_proposals SET schedule = ?, status = ?, proposal_message = ?, refusal_reason = ?, decided_at = ?
		WHERE id = ?`,
		string(spec), p.Status, p.ProposalMessage, p.RefusalReason, formatOptionalTime(p.DecidedAt), n)
	if err != nil {
		return fmt.Errorf("update schedule proposal %s: %w", p.ID, err)
	}

	return nil
}

// ListProposals returns every proposal, newest first.
func (s *Store) ListProposals(ctx context.Context) ([]schedule.Proposal, error) {
	rows, err := s.db.QueryContext(ctx, selectProposals+` ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list schedule proposals: %w", err)
	}
	defer rows.Close()

	var proposals []schedule.Proposal

	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}

		proposals = append(proposals, p)
	}

	return proposals, rows.Err()
}

const selectProposals = `
	SELECT id, schedule, status, proposed_by, proposal_message, refusal_reason, created_at, decided_at
	FROM schedule_proposals`

func scanProposal(row scanner) (schedule.Proposal, error) {
	var (
		p                          schedule.Proposal
		n                          int64
		spec, createdAt, decidedAt string
	)
	if err := row.Scan(&n, &spec, &p.Status, &p.ProposedBy, &p.ProposalMessage, &p.RefusalReason, &createdAt, &decidedAt); err != nil {
		return schedule.Proposal{}, err
	}

	p.ID = schedule.FormatProposalID(n)

	if err := json.Unmarshal([]byte(spec), &p.Schedule); err != nil {
		return schedule.Proposal{}, fmt.Errorf("read proposal %s: %w", p.ID, err)
	}

	var err error
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return schedule.Proposal{}, err
	}

	p.DecidedAt, err = parseOptionalTime(decidedAt)

	return p, err
}

// formatOptionalTime stores a missing time as "".
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return formatTime(*t)
}

func parseOptionalTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // a missing time is not an error
	}

	t, err := parseTime(s)
	if err != nil {
		return nil, err
	}

	return &t, nil
}
