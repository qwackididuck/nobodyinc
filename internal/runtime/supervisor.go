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

	mu     sync.Mutex
	agents map[string]*agentProcess
	// pending holds the changes waiting for an agent to end its turn (apply.go).
	pending map[string]pendingChange
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
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// RestartAll stops every agent and starts it again with a fresh session,
// e.g. so that it works in the worktree of a newly activated feature. Agents
// on leave stay stopped.
func (s *Supervisor) RestartAll(ctx context.Context) error {
	var errs []error

	for _, m := range s.Org.Current().Members() {
		if err := s.Stop(m.Address); err != nil {
			errs = append(errs, err)

			continue
		}

		if m.Agent.OnLeave {
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

	if s.agents == nil {
		s.agents = map[string]*agentProcess{}
	}

	if _, running := s.agents[address]; running {
		return fmt.Errorf("%w: %s", ErrAlreadyRunning, address)
	}

	cmd, err := s.NewCommand(ctx, o, member, resume)
	if err != nil {
		return err
	}

	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return fmt.Errorf("start agent %s: %w", address, err)
	}

	p := &agentProcess{cmd: cmd, terminal: terminal, output: NewOutput(), exited: make(chan struct{})}
	s.agents[address] = p
	s.Statuses.Set(address, StatusStarting)
	s.Logger.Info("agent started", "agent", address, "pid", cmd.Process.Pid, "resumed", resume != "")

	go s.watch(address, p)

	return nil
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
