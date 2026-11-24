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
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Errors of the shells.
var (
	ErrUnknownShell  = errors.New("unknown shell")
	ErrTooManyShells = errors.New("too many shells open: close one first")
)

// maxShells bounds how many shells a project keeps open at once.
const maxShells = 8

// shellStopTimeout bounds how long Close waits for a shell to hang up before
// killing it.
const shellStopTimeout = 2 * time.Second

// Shell describes an open shell, as the control room lists it.
type Shell struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Started time.Time `json:"started"`
}

// Shells are the plain terminals the human opens in the control room: the
// user's own shell in the project's folder, in a pseudo-terminal, with no
// agent behind it (ADR-0021). They live in the project daemon: closing the
// page does not end them, closing the project does.
type Shells struct {
	// Dir is where a shell starts: the project's folder.
	Dir    string
	Logger *slog.Logger
	// Shell is the program to run; empty runs the user's shell ($SHELL).
	Shell string

	mu      sync.Mutex
	items   map[string]*shellProcess
	counter int
	closed  bool
}

type shellProcess struct {
	info     Shell
	cmd      *exec.Cmd
	terminal *os.File
	output   *Output
	writeMu  sync.Mutex    // keeps keystrokes from interleaving
	exited   chan struct{} // closed once the process is gone
}

// Open starts a new shell and returns it. The shell does not end with ctx
// (usually a request's): it lives until it is closed.
func (s *Shells) Open(ctx context.Context) (Shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return Shell{}, ErrStopping
	}

	if len(s.items) >= maxShells {
		return Shell{}, ErrTooManyShells
	}

	s.counter++
	id := fmt.Sprintf("sh%d", s.counter)

	cmd := exec.CommandContext(context.WithoutCancel(ctx), s.program()) //nolint:gosec // the user's own shell: running it is the point
	cmd.Dir = s.Dir
	cmd.Env = shellEnv(os.Environ())

	terminal, err := startInPTY(cmd)
	if err != nil {
		return Shell{}, fmt.Errorf("start a shell: %w", err)
	}

	p := &shellProcess{
		info:     Shell{ID: id, Name: fmt.Sprintf("Shell %d", s.counter), Started: time.Now().UTC()},
		cmd:      cmd,
		terminal: terminal,
		output:   NewOutput(),
		exited:   make(chan struct{}),
	}

	if s.items == nil {
		s.items = map[string]*shellProcess{}
	}

	s.items[id] = p

	go s.watch(p)

	return p.info, nil
}

// List returns the open shells, oldest first.
func (s *Shells) List() []Shell {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := make([]Shell, 0, len(s.items))
	for _, p := range s.items {
		list = append(list, p.info)
	}

	sortShells(list)

	return list
}

// Close ends a shell: it gets a hangup, like a closed terminal window, and is
// killed if it does not leave.
func (s *Shells) Close(id string) error {
	p, ok := s.get(id)
	if !ok {
		return ErrUnknownShell
	}

	stopShell(p)

	return nil
}

// CloseAll ends every shell and refuses new ones: the project is closing.
func (s *Shells) CloseAll() {
	s.mu.Lock()
	s.closed = true
	all := make([]*shellProcess, 0, len(s.items))

	for _, p := range s.items {
		all = append(all, p)
	}
	s.mu.Unlock()

	for _, p := range all {
		stopShell(p)
	}
}

// Output returns the terminal output of an open shell.
func (s *Shells) Output(id string) (*Output, bool) {
	p, ok := s.get(id)
	if !ok {
		return nil, false
	}

	return p.output, true
}

// Input types raw bytes into a shell.
func (s *Shells) Input(id string, data []byte) error {
	p, ok := s.get(id)
	if !ok {
		return ErrUnknownShell
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	_, err := p.terminal.Write(data)

	return err
}

// Resize changes the size of a shell's terminal.
func (s *Shells) Resize(id string, rows, cols uint16) error {
	p, ok := s.get(id)
	if !ok {
		return ErrUnknownShell
	}

	return pty.Setsize(p.terminal, &pty.Winsize{Rows: rows, Cols: cols})
}

func (s *Shells) get(id string) (*shellProcess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.items[id]

	return p, ok
}

// watch copies a shell's output until its process ends, then forgets it: the
// viewers are let go, and the control room sees the shell is gone.
func (s *Shells) watch(p *shellProcess) {
	_, _ = io.Copy(p.output, p.terminal) // ends with an error when the process exits
	err := p.cmd.Wait()

	s.mu.Lock()
	delete(s.items, p.info.ID)
	s.mu.Unlock()

	p.terminal.Close()
	p.output.Close()
	close(p.exited)

	s.Logger.Info("shell exited", "shell", p.info.ID, "err", err)
}

// stopShell hangs a shell up, like closing a terminal window, and waits for it
// to leave, killing it if needed. The signals go to the shell's whole process
// group (it leads its own session), so what it runs goes too. Closing the
// pseudo-terminal would not do: the watcher is still reading it.
func stopShell(p *shellProcess) {
	signalGroup(p, syscall.SIGHUP)

	select {
	case <-p.exited:
		return
	case <-time.After(shellStopTimeout):
	}

	signalGroup(p, syscall.SIGKILL)

	<-p.exited
}

func signalGroup(p *shellProcess, sig syscall.Signal) {
	_ = syscall.Kill(-p.cmd.Process.Pid, sig) // a group already gone is fine
}

// program is the shell to run: the one configured, else the user's.
func (s *Shells) program() string {
	if s.Shell != "" {
		return s.Shell
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/sh"
}

// shellEnv is the environment of a shell: the daemon's, which is the user's,
// told it talks to a color terminal. The project's variables are not added
// (ADR-0020 keeps them out of the user's shell).
func shellEnv(base []string) []string {
	env := make([]string, 0, len(base)+2)

	for _, kv := range base {
		if hasName(kv, "TERM") || hasName(kv, "COLORTERM") {
			continue
		}

		env = append(env, kv)
	}

	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func hasName(kv, name string) bool {
	return len(kv) > len(name) && kv[:len(name)] == name && kv[len(name)] == '='
}
