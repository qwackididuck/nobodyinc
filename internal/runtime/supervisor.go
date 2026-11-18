package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// wakePrompt is typed into the terminal of an idle agent that just received
// mail. Submitting it fires the UserPromptSubmit hook, which attaches the mail
// to this very prompt (see docs/hooks.md). We only ever write to the agent's
// terminal, never read its screen.
const wakePrompt = `You have new mail: the Nobody Inc system attaches it to this message. Read it and act on it.`

// enterDelay separates the prompt from the Enter key, so that the terminal UI
// does not take them as a single pasted block.
const enterDelay = 300 * time.Millisecond

// Errors returned by the supervisor.
var (
	ErrUnknownAgent   = errors.New("unknown agent")
	ErrNotRunning     = errors.New("agent is not running")
	ErrAlreadyRunning = errors.New("agent is already running")
	ErrOnLeave        = errors.New("agent is on leave")
	ErrStopping       = errors.New("the daemon is shutting down")
)

// Supervisor runs one Claude Code session per agent, each in its own
// pseudo-terminal, and wakes idle agents up when mail arrives.
type Supervisor struct {
	Org      org.Source
	Statuses *Statuses
	Logger   *slog.Logger
	// NewCommand builds the process of an agent; usually Launch.Command. A
	// non-empty resume is the Claude Code session to continue.
	NewCommand func(ctx context.Context, o *org.Org, m org.Member, resume string) (*exec.Cmd, error)
	// HasMail reports whether an agent has undelivered mail.
	HasMail func(ctx context.Context, address string) (bool, error)
	// OnRetired, when set, is called once an agent has stopped because it is
	// on leave or no longer in the organization (apply.go), so that what it
	// leaves behind (tickets, mail) can be handed over.
	OnRetired func(address string)
	// OnStart, when set, is called whenever an agent's process is about to
	// start, before it can report anything: a resumed session keeps its ID
	// but its running cost starts again from 0 (budget.Meter.Forget). It is
	// called with the supervisor's lock held and must not call back into it.
	OnStart func(address string)

	mu     sync.Mutex
	agents map[string]*agentProcess
	// pending holds the changes waiting for an agent to end its turn (apply.go).
	pending map[string]pendingChange
	// stopping is set by StopAll: the daemon shuts down, nothing starts again.
	stopping bool
	// sessionDirs is the directory each agent's current session started in:
	// a session is only resumed there (start).
	sessionDirs map[string]string
}

type agentProcess struct {
	cmd      *exec.Cmd
	terminal *os.File
	output   *Output
	writeMu  sync.Mutex    // keeps keystrokes from interleaving
	exited   chan struct{} // closed once the process is gone
}

// stopTimeout bounds how long Stop waits for an agent's process to end.
const stopTimeout = 10 * time.Second

// StartAll starts every agent of the organization, except those on leave.
func (s *Supervisor) StartAll(ctx context.Context) error {
	var errs []error

	for _, m := range s.Org.Current().Members() {
		if m.Agent.OnLeave {
			continue
		}

		if err := s.Start(ctx, m.Address); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Start launches an agent in a new pseudo-terminal, with a fresh session.
// Its terminal output is recorded and its status follows the process:
// starting, then exited. An agent on leave is not started.
//
// ctx bounds the agent's process: it must live as long as the agent should.
func (s *Supervisor) Start(ctx context.Context, address string) error {
	return s.start(ctx, address, "")
}

// StopAll terminates every running agent.
//
// Changes waiting for an agent to end its turn are dropped, and no agent can
// start afterwards: the daemon is shutting down. A pending leave or dismissal
// is handed over at the next start instead (daemon/staff.go, reconcile).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopping = true
	s.pending = nil

	for address, p := range s.agents {
		if err := p.cmd.Process.Kill(); err != nil {
			s.Logger.Warn("stop agent", "agent", address, "err", err)
		}
	}
}

// Stop ends an agent's process and waits until it is gone. Stopping an agent
// that is not running does nothing.
func (s *Supervisor) Stop(address string) error {
	p, ok := s.process(address)
	if !ok {
		return nil
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("stop agent %s: %w", address, err)
	}

	select {
	case <-p.exited:
		return nil
	case <-time.After(stopTimeout):
		return fmt.Errorf("agent %s did not stop within %s", address, stopTimeout)
	}
}

// RestartAll stops every team agent and starts it again with a fresh session,
// e.g. so that it works in the worktree of a newly activated feature. Agents
// on leave stay stopped. The director is left alone: it always works in the
// project, and must keep the conversation in which the feature was agreed.
//
// Changes waiting for these agents are taken over: the restart already uses
// their current definition. An agent whose leave was waiting hands its work
// over (OnRetired), as Apply would have done.
func (s *Supervisor) RestartAll(ctx context.Context) error {
	var errs []error

	o := s.Org.Current()

	for _, m := range o.Members() {
		if o.IsGod(m.Address) {
			continue
		}

		hadPending := s.takePending(m.Address)

		if err := s.Stop(m.Address); err != nil {
			errs = append(errs, err)

			continue
		}

		if m.Agent.OnLeave {
			if hadPending && s.OnRetired != nil {
				s.OnRetired(m.Address)
			}

			continue
		}

		if err := s.Start(ctx, m.Address); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Output returns the terminal output of a running agent.
func (s *Supervisor) Output(address string) (*Output, bool) {
	p, ok := s.process(address)
	if !ok {
		return nil, false
	}

	return p.output, true
}

// Input types raw bytes into an agent's terminal, as a human at its keyboard.
func (s *Supervisor) Input(address string, data []byte) error {
	p, ok := s.process(address)
	if !ok {
		return ErrNotRunning
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	_, err := p.terminal.Write(data)

	return err
}

// Resize changes the size of an agent's terminal. Claude Code redraws its
// screen on resize, which is also how a new viewer gets a clean picture.
func (s *Supervisor) Resize(address string, rows, cols uint16) error {
	p, ok := s.process(address)
	if !ok {
		return ErrNotRunning
	}

	return pty.Setsize(p.terminal, &pty.Winsize{Rows: rows, Cols: cols})
}

// NotifyMail is called when mail arrives for an agent. If the agent is idle,
// it is woken up so that the mail gets delivered; a busy agent will get it
// from its hooks anyway.
func (s *Supervisor) NotifyMail(address string) {
	if s.Statuses.Get(address) != StatusIdle {
		return
	}
	// Marked working before typing, so a second message does not wake it twice.
	s.Statuses.Set(address, StatusWorking)
	go func() {
		if err := s.typePrompt(address, wakePrompt); err != nil {
			s.Logger.Warn("wake agent", "agent", address, "err", err)
		}
	}()
}

// OnIdle is called when an agent ends its turn. A change waiting for this
// moment is applied (apply.go). Otherwise, mail that arrived just as the turn
// was ending would wait until the next turn: the agent is woken up for it.
func (s *Supervisor) OnIdle(address string) {
	if s.applyIfPending(address) {
		return // the restarted agent gets its mail with its first prompt
	}

	hasMail, err := s.HasMail(context.Background(), address)
	if err != nil {
		s.Logger.Warn("check mail of idle agent", "agent", address, "err", err)

		return
	}

	if hasMail {
		s.NotifyMail(address)
	}
}

// start launches an agent, continuing the given session if resume is set.
func (s *Supervisor) start(ctx context.Context, address, resume string) error {
	// One snapshot for the whole start, so the member and the prompt's
	// contacts come from the same organization.
	o := s.Org.Current()

	member, ok := o.Member(address)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAgent, address)
	}

	if member.Agent.OnLeave {
		return fmt.Errorf("%w: %s", ErrOnLeave, address)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping {
		return fmt.Errorf("%w: %s", ErrStopping, address)
	}

	if s.agents == nil {
		s.agents = map[string]*agentProcess{}
	}

	if _, running := s.agents[address]; running {
		return fmt.Errorf("%w: %s", ErrAlreadyRunning, address)
	}

	cmd, resumed, err := s.command(ctx, o, member, resume)
	if err != nil {
		return err
	}

	if s.OnStart != nil {
		s.OnStart(address)
	}

	terminal, err := startInPTY(cmd)
	if err != nil {
		return fmt.Errorf("start agent %s: %w", address, err)
	}

	p := &agentProcess{cmd: cmd, terminal: terminal, output: NewOutput(), exited: make(chan struct{})}
	s.agents[address] = p
	s.Statuses.Set(address, StatusStarting)
	s.Logger.Info("agent started", "agent", address, "pid", cmd.Process.Pid, "resumed", resumed)

	go s.watch(address, p)

	return nil
}

// ptyAttempts and ptyRetryDelay bound the retries of startInPTY.
const (
	ptyAttempts   = 3
	ptyRetryDelay = 50 * time.Millisecond
)

// startInPTY starts cmd in a new pseudo-terminal. Opening a pseudo-terminal
// sometimes fails for a moment on macOS when several are opened at once
// (seen as "errno -6" while agents and tests start together): it is retried
// briefly, as long as the process itself was not started.
func startInPTY(cmd *exec.Cmd) (*os.File, error) {
	var err error

	for range ptyAttempts {
		var terminal *os.File

		terminal, err = pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
		if err == nil || cmd.Process != nil {
			return terminal, err
		}

		time.Sleep(ptyRetryDelay)
	}

	return nil, err
}

// command builds an agent's process, resuming the given session when it can,
// and reports whether it does. Called with s.mu held.
//
// Claude Code keeps sessions per directory: a session started in another
// directory (e.g. before a feature moved the agent into its worktree) cannot
// be resumed, so the agent then starts afresh.
func (s *Supervisor) command(ctx context.Context, o *org.Org, m org.Member, resume string) (*exec.Cmd, bool, error) {
	cmd, err := s.NewCommand(ctx, o, m, resume)
	if err != nil || resume == "" {
		s.recordSessionDir(m.Address, cmd)

		return cmd, false, err
	}

	if cmd.Dir == s.sessionDirs[m.Address] {
		return cmd, true, nil
	}

	s.Logger.Info("agent moved to another directory: fresh session instead of resuming",
		"agent", m.Address, "from", s.sessionDirs[m.Address], "to", cmd.Dir)

	cmd, err = s.NewCommand(ctx, o, m, "")
	s.recordSessionDir(m.Address, cmd)

	return cmd, false, err
}

// recordSessionDir remembers where a fresh session starts. Called with s.mu
// held.
func (s *Supervisor) recordSessionDir(address string, cmd *exec.Cmd) {
	if cmd == nil {
		return
	}

	if s.sessionDirs == nil {
		s.sessionDirs = map[string]string{}
	}

	s.sessionDirs[address] = cmd.Dir
}

// watch copies the agent's terminal output until the process ends.
func (s *Supervisor) watch(address string, p *agentProcess) {
	_, _ = io.Copy(p.output, p.terminal) // ends with an error when the process exits
	err := p.cmd.Wait()

	s.mu.Lock()
	delete(s.agents, address)
	s.mu.Unlock()

	p.terminal.Close()
	_, _ = fmt.Fprintf(p.output, "\r\n[nobody inc] agent process exited: %v\r\n", err) // Output.Write never fails
	p.output.Close()                                                                   // its terminal viewers reconnect to the next session

	s.Statuses.Set(address, StatusExited)
	s.Logger.Info("agent exited", "agent", address, "err", err)
	close(p.exited)

	// An agent that exits on its own (crash, /exit) while a change waits for
	// the end of its turn will never be idle: the change is applied now.
	s.applyIfPending(address)
}

// typePrompt types a prompt into an agent's terminal and presses Enter.
func (s *Supervisor) typePrompt(address, text string) error {
	if err := s.Input(address, []byte(text)); err != nil {
		return err
	}

	time.Sleep(enterDelay)

	return s.Input(address, []byte("\r"))
}

func (s *Supervisor) process(address string) (*agentProcess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.agents[address]

	return p, ok
}
