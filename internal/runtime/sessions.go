package runtime

import "context"

// Resuming sessions (ADR-0015).
//
// Claude Code keeps every session on disk, and claude --resume <session>
// continues one. The supervisor resumes an agent's session in two cases:
//
//   - a change to the agent while it runs (apply.go): the session resumes
//     with a first prompt carrying the current organization
//     (ResumeAfterChange, resumeMessage);
//   - the project opens again (StartAll): every agent whose session the
//     daemon remembers (SessionStore) and that still works in the same
//     directory resumes it. An agent that was idle resumes silently, without
//     a prompt (ResumeSilently): its SessionStart hook hands it the current
//     organization and makes it idle, so nothing is paid until it has
//     something to do. An agent stopped in the middle of a turn resumes with
//     a short prompt to carry on (ResumeInterrupted).
//
// A session Claude Code no longer has makes claude exit at once, before its
// SessionStart hook: the supervisor then starts the agent afresh (watch).

// ResumeMode says how a resumed session opens.
type ResumeMode int

// The ways a session resumes.
const (
	// ResumeAfterChange: the agent was changed while it ran; its first
	// prompt carries the current organization (resumeMessage).
	ResumeAfterChange ResumeMode = iota
	// ResumeSilently: the project opened again while the agent was idle; no
	// prompt, the SessionStart hook does the rest.
	ResumeSilently
	// ResumeInterrupted: the project opened again after stopping in the
	// middle of the agent's turn; a short prompt tells it to carry on.
	ResumeInterrupted
)

// Resume names the Claude Code session an agent continues, and how. The zero
// value starts a fresh session.
type Resume struct {
	Session string
	Mode    ResumeMode
}

// restoring reports whether the resume reopens a project (as opposed to a
// change while the agent ran): its SessionStart hook then hands the agent
// the current organization.
func (r Resume) restoring() bool {
	return r.Session != "" && r.Mode != ResumeAfterChange
}

// SavedSession is an agent's session as the daemon remembers it.
type SavedSession struct {
	ID string
	// WorkDir is where the session started: it is only resumed there.
	WorkDir string
	// MidTurn: the agent was in the middle of a turn when last seen.
	MidTurn bool
}

// SessionStore keeps the agents' sessions across daemon restarts
// (implemented over the database by the daemon).
type SessionStore interface {
	// Sessions returns the recorded sessions, by agent address.
	Sessions(ctx context.Context) (map[string]SavedSession, error)
	// SaveSession records the session an agent runs; a new session starts
	// out of a turn.
	SaveSession(ctx context.Context, address, sessionID, workDir string) error
	// SetMidTurn records whether an agent is in the middle of a turn.
	SetMidTurn(ctx context.Context, address string, midTurn bool) error
	// ForgetSession removes an agent's session: its next start is fresh.
	ForgetSession(ctx context.Context, address string) error
}

// SessionStarted is called on an agent's SessionStart hook. For the first
// SessionStart of a session resumed because the project opened again, it
// returns the context to hand the agent (ReopenContext), and whether the
// agent is now idle: a silent resume runs no turn. Otherwise it returns ""
// and false.
func (s *Supervisor) SessionStarted(address string) (string, bool) {
	s.mu.Lock()

	var (
		resume Resume
		first  bool
	)

	if p, ok := s.agents[address]; ok {
		first = !p.sessionStarted
		p.sessionStarted = true
		resume = p.resume
	}
	s.mu.Unlock()

	if !first || !resume.restoring() {
		return "", false
	}

	idle := resume.Mode == ResumeSilently

	if s.ReopenContext == nil {
		return "", idle
	}

	o := s.Org.Current()

	m, ok := o.Member(address)
	if !ok {
		return "", idle
	}

	reopen, err := s.ReopenContext(o, m)
	if err != nil {
		s.Logger.Warn("build the context of a reopened session", "agent", address, "err", err)
	}

	return reopen, idle
}

// RememberSession records the Claude Code session an agent runs, learnt from
// its hooks, with the directory it started in (Statuses.OnSession).
func (s *Supervisor) RememberSession(ctx context.Context, address, sessionID string) {
	if s.Sessions == nil {
		return
	}

	s.mu.Lock()
	dir := s.sessionDirs[address]
	s.mu.Unlock()

	if err := s.Sessions.SaveSession(ctx, address, sessionID, dir); err != nil {
		s.Logger.Warn("remember the agent's session", "agent", address, "err", err)
	}
}

// ObserveStatus records whether an agent is in the middle of a turn, so that
// a project opening again knows which turns were interrupted
// (Statuses.OnChange). Starts and exits say nothing about the turn and are
// ignored: an agent stopped with the daemon keeps what it was doing.
func (s *Supervisor) ObserveStatus(ctx context.Context, address string, st Status) {
	if s.Sessions == nil {
		return
	}

	var midTurn bool

	switch st {
	case StatusWorking, StatusAwaitingApproval:
		midTurn = true
	case StatusIdle, StatusBudgetHold:
		midTurn = false
	case StatusStopped, StatusStarting, StatusExited:
		return
	}

	if err := s.Sessions.SetMidTurn(ctx, address, midTurn); err != nil {
		s.Logger.Warn("record whether the agent is mid-turn", "agent", address, "err", err)
	}
}

// savedSessions returns the sessions the daemon remembers; none if it
// remembers nothing or cannot read them.
func (s *Supervisor) savedSessions(ctx context.Context) map[string]SavedSession {
	if s.Sessions == nil {
		return nil
	}

	saved, err := s.Sessions.Sessions(ctx)
	if err != nil {
		s.Logger.Error("read the agents' sessions: every agent starts afresh", "err", err)

		return nil
	}

	return saved
}

// reopening returns how an agent starts when the project opens: resuming
// its remembered session, silently if it was idle, with a prompt to carry
// on if its turn was interrupted; fresh if there is none. It remembers where
// that session started: command resumes it only there.
func (s *Supervisor) reopening(address string, saved map[string]SavedSession) Resume {
	session, ok := saved[address]
	if !ok || session.ID == "" {
		return Resume{}
	}

	s.mu.Lock()
	s.setSessionDir(address, session.WorkDir)
	s.mu.Unlock()

	if session.MidTurn {
		return Resume{Session: session.ID, Mode: ResumeInterrupted}
	}

	return Resume{Session: session.ID, Mode: ResumeSilently}
}

// setSessionDir remembers where an agent's session started. Called with
// s.mu held.
func (s *Supervisor) setSessionDir(address, dir string) {
	if s.sessionDirs == nil {
		s.sessionDirs = map[string]string{}
	}

	s.sessionDirs[address] = dir
}

// forgetSession drops the session the daemon remembered for an agent.
func (s *Supervisor) forgetSession(ctx context.Context, address string) {
	if s.Sessions == nil {
		return
	}

	if err := s.Sessions.ForgetSession(ctx, address); err != nil {
		s.Logger.Warn("forget the agent's session", "agent", address, "err", err)
	}
}

// startAfresh starts with a fresh session an agent whose resumed session
// turned out to be lost (watch).
func (s *Supervisor) startAfresh(address string, p *agentProcess) {
	s.Logger.Warn("the agent's session could not be resumed: fresh session instead",
		"agent", address, "session", p.resume.Session)

	s.forgetSession(p.ctx, address)

	if err := s.start(p.ctx, address, Resume{}); err != nil {
		s.Logger.Error("start agent afresh", "agent", address, "err", err)
	}
}
