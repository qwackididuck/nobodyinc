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
)

// Supervisor runs one Claude Code session per agent, each in its own
// pseudo-terminal, and wakes idle agents up when mail arrives.
type Supervisor struct {
	Org      *org.Org
	Statuses *Statuses
	Logger   *slog.Logger
	// NewCommand builds the process of an agent; usually Launch.Command.
	NewCommand func(ctx context.Context, o *org.Org, m org.Member) (*exec.Cmd, error)
	// HasMail reports whether an agent has undelivered mail.
	HasMail func(ctx context.Context, address string) (bool, error)

	mu     sync.Mutex
	agents map[string]*agentProcess
}

type agentProcess struct {
	cmd      *exec.Cmd
	terminal *os.File
	output   *Output
	writeMu  sync.Mutex // keeps keystrokes from interleaving
}

// StartAll starts every agent of the organization.
func (s *Supervisor) StartAll(ctx context.Context) error {
	var errs []error

	for _, m := range s.Org.Members() {
		if err := s.Start(ctx, m.Address); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Start launches an agent in a new pseudo-terminal. Its terminal output is
// recorded and its status follows the process: starting, then exited.
func (s *Supervisor) Start(ctx context.Context, address string) error {
	member, ok := s.Org.Member(address)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAgent, address)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.agents == nil {
		s.agents = map[string]*agentProcess{}
	}

	if _, running := s.agents[address]; running {
		return fmt.Errorf("%w: %s", ErrAlreadyRunning, address)
	}

	cmd, err := s.NewCommand(ctx, s.Org, member)
	if err != nil {
		return err
	}

	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return fmt.Errorf("start agent %s: %w", address, err)
	}

	p := &agentProcess{cmd: cmd, terminal: terminal, output: NewOutput()}
	s.agents[address] = p
	s.Statuses.Set(address, StatusStarting)
	s.Logger.Info("agent started", "agent", address, "pid", cmd.Process.Pid)

	go s.watch(address, p)

	return nil
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

// OnIdle is called when an agent ends its turn. Mail that arrived just as the
// turn was ending would otherwise wait until the next turn.
func (s *Supervisor) OnIdle(address string) {
	hasMail, err := s.HasMail(context.Background(), address)
	if err != nil {
		s.Logger.Warn("check mail of idle agent", "agent", address, "err", err)

		return
	}

	if hasMail {
		s.NotifyMail(address)
	}
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
