package store

import (
	"context"
	"fmt"
	"time"
)

// AgentSession is the Claude Code session an agent runs, as the daemon
// remembers it across restarts (ADR-0015).
type AgentSession struct {
	Address   string
	SessionID string
	// WorkDir is the directory the session started in: Claude Code keeps
	// sessions per directory, so it can only be resumed there.
	WorkDir string
	// MidTurn is set while the agent is in the middle of a turn: still set
	// when the daemon stopped, it means the turn was interrupted.
	MidTurn bool
}

// SaveAgentSession records the session an agent runs. A new session starts
// out of a turn; the same session keeps its mid-turn flag.
func (s *Store) SaveAgentSession(ctx context.Context, address, sessionID, workDir string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_sessions (address, session_id, work_dir, mid_turn, updated_at)
		VALUES (?, ?, ?, 0, ?)
		ON CONFLICT (address) DO UPDATE SET
			mid_turn   = CASE WHEN session_id = excluded.session_id THEN mid_turn ELSE 0 END,
			session_id = excluded.session_id,
			work_dir   = excluded.work_dir,
			updated_at = excluded.updated_at`,
		address, sessionID, workDir, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("save the session of %s: %w", address, err)
	}

	return nil
}

// SetAgentMidTurn records whether an agent is in the middle of a turn. An
// agent without a recorded session is left alone.
func (s *Store) SetAgentMidTurn(ctx context.Context, address string, midTurn bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET mid_turn = ?, updated_at = ? WHERE address = ?`,
		midTurn, formatTime(time.Now()), address)
	if err != nil {
		return fmt.Errorf("record whether %s is mid-turn: %w", address, err)
	}

	return nil
}

// ForgetAgentSession removes an agent's session: its next start is fresh.
func (s *Store) ForgetAgentSession(ctx context.Context, address string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_sessions WHERE address = ?`, address); err != nil {
		return fmt.Errorf("forget the session of %s: %w", address, err)
	}

	return nil
}

// AgentSessions returns every recorded session.
func (s *Store) AgentSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT address, session_id, work_dir, mid_turn FROM agent_sessions ORDER BY address`)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	defer rows.Close()

	var out []AgentSession

	for rows.Next() {
		var a AgentSession
		if err := rows.Scan(&a.Address, &a.SessionID, &a.WorkDir, &a.MidTurn); err != nil {
			return nil, fmt.Errorf("read agent session: %w", err)
		}

		out = append(out, a)
	}

	return out, rows.Err()
}
