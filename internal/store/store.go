// Package store persists Nobody Inc's state in a single SQLite file.
//
// It holds the messages (pending in their recipient's tray until marked
// delivered) and the tickets with their deliverables and comments (see
// tickets.go). The schema evolves through the files in migrations/, applied
// on Open (see migrate.go).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, keeps the daemon a single binary

	"github.com/qwackididuck/nobodyinc/internal/messaging"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Store is the SQLite-backed state of the daemon.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path and brings its schema up to date.
// Use ":memory:" for a throwaway database in tests.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite allows a single writer; one connection avoids "database is locked"
	// errors and keeps :memory: databases shared.
	db.SetMaxOpenConns(1)

	if err := migrate(ctx, db); err != nil {
		db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// InsertMessage files a new message in its recipient's tray.
func (s *Store) InsertMessage(ctx context.Context, m messaging.Message) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (id, conversation, in_reply_to, sender, recipient, act, priority, subject, body, hops, sent_at, ticket)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Conversation, m.InReplyTo, m.From, m.To, m.Act, m.Priority, m.Subject, m.Body, m.Hops, formatTime(m.SentAt), m.Ticket)
	if err != nil {
		return fmt.Errorf("insert message %s: %w", m.ID, err)
	}

	return nil
}

// GetMessage returns one message by ID.
func (s *Store) GetMessage(ctx context.Context, id string) (messaging.Message, error) {
	row := s.db.QueryRowContext(ctx, selectMessages+` WHERE id = ?`, id)

	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return messaging.Message{}, fmt.Errorf("message %s: %w", id, ErrNotFound)
	}

	return m, err
}

// PendingMessages returns the undelivered messages in a recipient's tray,
// oldest first.
func (s *Store) PendingMessages(ctx context.Context, recipient string, p messaging.Priority) ([]messaging.Message, error) {
	return s.queryMessages(ctx, selectMessages+`
		WHERE recipient = ? AND priority = ? AND delivered_at IS NULL
		ORDER BY sent_at, id`, recipient, p)
}

// Tray is one recipient's view of the messages; it is what the hooks read.
type Tray struct {
	store     *Store
	recipient string
}

// Tray returns the trays of one recipient.
func (s *Store) Tray(recipient string) Tray {
	return Tray{store: s, recipient: recipient}
}

// Pending returns the undelivered messages of the recipient for a priority.
func (t Tray) Pending(ctx context.Context, p messaging.Priority) ([]messaging.Message, error) {
	return t.store.PendingMessages(ctx, t.recipient, p)
}

// TicketMessages returns the messages about a ticket, oldest first.
func (s *Store) TicketMessages(ctx context.Context, ticket string) ([]messaging.Message, error) {
	return s.queryMessages(ctx, selectMessages+` WHERE ticket = ? ORDER BY sent_at, id`, ticket)
}

// RecentMessages returns the latest messages of the whole organization,
// oldest first, for the supervision UI.
func (s *Store) RecentMessages(ctx context.Context, limit int) ([]messaging.Message, error) {
	latest := selectMessages + ` ORDER BY sent_at DESC, id DESC LIMIT ?`

	return s.queryMessages(ctx, `SELECT `+messageColumns+` FROM (`+latest+`) ORDER BY sent_at, id`, limit)
}

// MarkDelivered records that messages have been handed to their recipient.
// IDs addressed to someone else are ignored, and already delivered messages
// keep their original delivery time.
func (s *Store) MarkDelivered(ctx context.Context, recipient string, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}

	const update = "UPDATE messages SET delivered_at = ? WHERE recipient = ? AND delivered_at IS NULL AND id IN "

	in, idArgs := inClause(ids)
	query := update + in //nolint:gosec // in holds only placeholders

	args := append([]any{formatTime(at), recipient}, idArgs...)

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark messages delivered: %w", err)
	}

	return nil
}

// inClause returns "(?, ?, …)" for ids and the matching query arguments.
func inClause(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}

// Requeue puts delivered messages back in their recipient's tray.
func (s *Store) Requeue(ctx context.Context, recipient string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	const update = "UPDATE messages SET delivered_at = NULL WHERE recipient = ? AND id IN "

	in, idArgs := inClause(ids)
	query := update + in //nolint:gosec // in holds only placeholders

	args := append([]any{recipient}, idArgs...)

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("requeue messages: %w", err)
	}

	return nil
}

// messageColumns are the columns scanMessage reads, in order.
const messageColumns = `id, conversation, in_reply_to, sender, recipient, act, priority, subject, body, hops, sent_at, delivered_at, ticket`

const selectMessages = `SELECT ` + messageColumns + ` FROM messages`

type scanner interface {
	Scan(dest ...any) error
}

func scanMessage(row scanner) (messaging.Message, error) {
	var (
		m           messaging.Message
		sentAt      string
		deliveredAt sql.NullString
	)

	err := row.Scan(&m.ID, &m.Conversation, &m.InReplyTo, &m.From, &m.To, &m.Act, &m.Priority,
		&m.Subject, &m.Body, &m.Hops, &sentAt, &deliveredAt, &m.Ticket)
	if err != nil {
		return messaging.Message{}, err
	}

	if m.SentAt, err = parseTime(sentAt); err != nil {
		return messaging.Message{}, err
	}

	if deliveredAt.Valid {
		t, err := parseTime(deliveredAt.String)
		if err != nil {
			return messaging.Message{}, err
		}

		m.DeliveredAt = &t
	}

	return m, nil
}

func (s *Store) queryMessages(ctx context.Context, query string, args ...any) ([]messaging.Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []messaging.Message

	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("read message: %w", err)
		}

		messages = append(messages, m)
	}

	return messages, rows.Err()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", s, err)
	}

	return t, nil
}
