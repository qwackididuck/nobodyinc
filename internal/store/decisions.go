package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/automation"
)

// InsertDecision records a decision of the director's. A question has at most
// one: a second returns automation.ErrDecided.
func (s *Store) InsertDecision(ctx context.Context, d automation.Decision) (automation.Decision, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE message = ?`, d.Message).Scan(&n); err != nil {
		return automation.Decision{}, fmt.Errorf("look for the decision on %s: %w", d.Message, err)
	}

	if n > 0 {
		return automation.Decision{}, fmt.Errorf("%w (message %s)", automation.ErrDecided, d.Message)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO decisions (asked_by, message, question, decision, rationale, impact, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.AskedBy, d.Message, d.Question, d.Decision, d.Rationale, d.Impact, formatTime(d.DecidedAt))
	if err != nil {
		return automation.Decision{}, fmt.Errorf("insert decision: %w", err)
	}

	if d.ID, err = res.LastInsertId(); err != nil {
		return automation.Decision{}, err
	}

	return d, nil
}

// ListDecisions returns the decisions, newest first.
func (s *Store) ListDecisions(ctx context.Context) ([]automation.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, asked_by, message, question, decision, rationale, impact, decided_at, seen
		FROM decisions ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	defer rows.Close()

	decisions := []automation.Decision{}

	for rows.Next() {
		var (
			d     automation.Decision
			at    string
			impct string
		)

		if err := rows.Scan(&d.ID, &d.AskedBy, &d.Message, &d.Question, &d.Decision, &d.Rationale, &impct, &at, &d.Seen); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}

		d.Impact = automation.Impact(impct)

		if d.DecidedAt, err = parseTime(at); err != nil {
			return nil, err
		}

		decisions = append(decisions, d)
	}

	return decisions, rows.Err()
}

// MarkDecisionsSeen acknowledges the decisions with these IDs, or every one
// when ids is empty.
func (s *Store) MarkDecisionsSeen(ctx context.Context, ids []int64) error {
	query, args := `UPDATE decisions SET seen = 1`, []any{}

	if len(ids) > 0 {
		// Only placeholders are concatenated: the IDs are passed as arguments.
		query += ` WHERE id IN (?` + strings.Repeat(`,?`, len(ids)-1) + `)` //nolint:gosec // placeholders only

		for _, id := range ids {
			args = append(args, id)
		}
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("mark decisions seen: %w", err)
	}

	return nil
}
