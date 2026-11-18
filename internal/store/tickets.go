package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/work"
)

// InsertTicket stores a new ticket with its dependencies and returns it with
// its ID.
func (s *Store) InsertTicket(ctx context.Context, t work.Ticket) (work.Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return work.Ticket{}, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	res, err := tx.ExecContext(ctx, `
		INSERT INTO tickets (title, description, acceptance_criteria, team, feature, assignee, status, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, t.Description, t.AcceptanceCriteria, t.Team, t.Feature, t.Assignee, t.Status, t.CreatedBy,
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	if err != nil {
		return work.Ticket{}, fmt.Errorf("insert ticket: %w", err)
	}

	n, err := res.LastInsertId()
	if err != nil {
		return work.Ticket{}, err
	}

	t.ID = work.FormatID(n)

	for _, dep := range t.BlockedBy {
		depN, err := work.ParseID(dep)
		if err != nil {
			return work.Ticket{}, err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_dependencies (ticket, depends_on) VALUES (?, ?)`, n, depN); err != nil {
			return work.Ticket{}, fmt.Errorf("insert dependency: %w", err)
		}
	}

	return t, tx.Commit()
}

// GetTicket returns one ticket with its dependencies.
func (s *Store) GetTicket(ctx context.Context, id string) (work.Ticket, error) {
	n, err := work.ParseID(id)
	if err != nil {
		return work.Ticket{}, err
	}

	t, err := scanTicket(s.db.QueryRowContext(ctx, selectTickets+` WHERE id = ?`, n))
	if errors.Is(err, sql.ErrNoRows) {
		return work.Ticket{}, fmt.Errorf("%w: %s", work.ErrNotFound, work.FormatID(n))
	}

	if err != nil {
		return work.Ticket{}, err
	}

	t.BlockedBy, err = s.dependencies(ctx, n)

	return t, err
}

// UpdateTicket saves a ticket's mutable fields.
func (s *Store) UpdateTicket(ctx context.Context, t work.Ticket) error {
	n, err := work.ParseID(t.ID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE tickets SET title = ?, description = ?, acceptance_criteria = ?, assignee = ?, status = ?, updated_at = ?
		WHERE id = ?`,
		t.Title, t.Description, t.AcceptanceCriteria, t.Assignee, t.Status, formatTime(t.UpdatedAt), n)
	if err != nil {
		return fmt.Errorf("update ticket %s: %w", t.ID, err)
	}

	return nil
}

// ListTickets returns the tickets matching a filter, oldest first.
func (s *Store) ListTickets(ctx context.Context, f work.Filter) ([]work.Ticket, error) {
	where, args := ticketFilter(f)

	tickets, err := s.queryTickets(ctx, selectTickets+where+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}

	// Dependencies are loaded once the cursor is closed: the store uses a
	// single connection.
	for i := range tickets {
		n, _ := work.ParseID(tickets[i].ID) // IDs come from FormatID
		if tickets[i].BlockedBy, err = s.dependencies(ctx, n); err != nil {
			return nil, err
		}
	}

	return tickets, nil
}

// ticketFilter turns a filter into a WHERE clause made of fixed conditions,
// with the values passed as arguments. Archived tickets are only listed when
// asked for, and then alone.
func ticketFilter(f work.Filter) (string, []any) {
	conditions := []string{"archived = ?"}
	args := []any{f.Archived}

	for column, value := range map[string]string{"team": f.Team, "assignee": f.Assignee, "status": string(f.Status)} {
		if value != "" {
			conditions = append(conditions, column+" = ?")
			args = append(args, value)
		}
	}

	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *Store) queryTickets(ctx context.Context, query string, args ...any) ([]work.Ticket, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	defer rows.Close()

	var tickets []work.Ticket

	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}

		tickets = append(tickets, t)
	}

	return tickets, rows.Err()
}

// InsertDeliverable stores a deliverable and returns it with its ID.
func (s *Store) InsertDeliverable(ctx context.Context, d work.Deliverable) (work.Deliverable, error) {
	n, err := work.ParseID(d.Ticket)
	if err != nil {
		return work.Deliverable{}, err
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO deliverables (ticket, author, kind, reference, summary, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		n, d.Author, d.Kind, d.Reference, d.Summary, formatTime(d.CreatedAt))
	if err != nil {
		return work.Deliverable{}, fmt.Errorf("insert deliverable: %w", err)
	}

	d.ID, err = res.LastInsertId()

	return d, err
}

// ListDeliverables returns the deliverables of a ticket, oldest first.
func (s *Store) ListDeliverables(ctx context.Context, ticket string) ([]work.Deliverable, error) {
	n, err := work.ParseID(ticket)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, author, kind, reference, summary, created_at FROM deliverables WHERE ticket = ? ORDER BY id`, n)
	if err != nil {
		return nil, fmt.Errorf("list deliverables: %w", err)
	}
	defer rows.Close()

	var list []work.Deliverable

	for rows.Next() {
		d := work.Deliverable{Ticket: work.FormatID(n)}

		var createdAt string
		if err := rows.Scan(&d.ID, &d.Author, &d.Kind, &d.Reference, &d.Summary, &createdAt); err != nil {
			return nil, err
		}

		if d.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}

		list = append(list, d)
	}

	return list, rows.Err()
}

// InsertComment stores a comment and returns it with its ID.
func (s *Store) InsertComment(ctx context.Context, c work.Comment) (work.Comment, error) {
	n, err := work.ParseID(c.Ticket)
	if err != nil {
		return work.Comment{}, err
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ticket_comments (ticket, author, body, created_at) VALUES (?, ?, ?, ?)`,
		n, c.Author, c.Body, formatTime(c.CreatedAt))
	if err != nil {
		return work.Comment{}, fmt.Errorf("insert comment: %w", err)
	}

	c.ID, err = res.LastInsertId()

	return c, err
}

// ListComments returns the comments of a ticket, oldest first.
func (s *Store) ListComments(ctx context.Context, ticket string) ([]work.Comment, error) {
	n, err := work.ParseID(ticket)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, author, body, created_at FROM ticket_comments WHERE ticket = ? ORDER BY id`, n)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	var list []work.Comment

	for rows.Next() {
		c := work.Comment{Ticket: work.FormatID(n)}

		var createdAt string
		if err := rows.Scan(&c.ID, &c.Author, &c.Body, &createdAt); err != nil {
			return nil, err
		}

		if c.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}

		list = append(list, c)
	}

	return list, rows.Err()
}

// ArchiveFeatureTickets archives the tickets of a feature and returns their
// IDs.
func (s *Store) ArchiveFeatureTickets(ctx context.Context, feature string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `UPDATE tickets SET archived = 1 WHERE feature = ? AND archived = 0 RETURNING id`, feature)
	if err != nil {
		return nil, fmt.Errorf("archive the tickets of %s: %w", feature, err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}

		ids = append(ids, work.FormatID(n))
	}

	return ids, rows.Err()
}

// DeleteTickets deletes tickets for good, with their comments, deliverables
// and dependencies (tickets that waited for them no longer do). Messages
// about them stay, without the link.
func (s *Store) DeleteTickets(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	numbers := make([]string, len(ids))

	for i, id := range ids {
		n, err := work.ParseID(id)
		if err != nil {
			return err
		}

		numbers[i] = strconv.FormatInt(n, 10)
	}

	nIn, nArgs := inClause(numbers)
	idIn, idArgs := inClause(ids)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM deliverables WHERE ticket IN " + nIn, nArgs},
		{"DELETE FROM ticket_comments WHERE ticket IN " + nIn, nArgs},
		{"DELETE FROM ticket_dependencies WHERE ticket IN " + nIn + " OR depends_on IN " + nIn, append(append([]any{}, nArgs...), nArgs...)},
		{"UPDATE messages SET ticket = '' WHERE ticket IN " + idIn, idArgs},
		{"DELETE FROM tickets WHERE id IN " + nIn, nArgs},
	}

	for _, st := range statements {
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return fmt.Errorf("delete tickets: %w", err)
		}
	}

	return tx.Commit()
}

const selectTickets = `
	SELECT id, title, description, acceptance_criteria, team, feature, assignee, status, created_by, created_at, updated_at, archived,
		` + ticketSpent + `
	FROM tickets`

func scanTicket(row scanner) (work.Ticket, error) {
	var (
		t                    work.Ticket
		n                    int64
		createdAt, updatedAt string
	)
	if err := row.Scan(&n, &t.Title, &t.Description, &t.AcceptanceCriteria, &t.Team, &t.Feature, &t.Assignee,
		&t.Status, &t.CreatedBy, &createdAt, &updatedAt, &t.Archived, &t.SpentUSD); err != nil {
		return work.Ticket{}, err
	}

	t.ID = work.FormatID(n)

	var err error
	if t.CreatedAt, err = parseTime(createdAt); err != nil {
		return work.Ticket{}, err
	}

	if t.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return work.Ticket{}, err
	}

	return t, nil
}

func (s *Store) dependencies(ctx context.Context, n int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT depends_on FROM ticket_dependencies WHERE ticket = ? ORDER BY depends_on`, n)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}
	defer rows.Close()

	var deps []string

	for rows.Next() {
		var dep int64
		if err := rows.Scan(&dep); err != nil {
			return nil, err
		}

		deps = append(deps, work.FormatID(dep))
	}

	return deps, rows.Err()
}
