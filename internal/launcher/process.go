package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// How the launcher waits on a project daemon.
const (
	healthPollInterval = 100 * time.Millisecond
	stopTimeout        = 30 * time.Second
	logTailBytes       = 2048
)

// process is one running project daemon: a child `nobodyd project` process
// (ADR-0014).
//
// The launcher opens the child's listening socket itself (127.0.0.1, a free
// port) and hands it over as file descriptor 3: no port can be taken in
// between, and the child knows its own URL before its agents start. The
// child's standard input is a pipe from the launcher: if the launcher dies,
// the pipe closes and the child stops, so no project daemon outlives it.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	url    *url.URL
	proxy  *httputil.ReverseProxy
	tail   *tailBuffer
	exited chan struct{}
	// err is how the process ended, set before exited is closed.
	err error
}

// processConfig says how to start a project daemon.
type processConfig struct {
	executable string
	// home is the user's Nobody Inc directory, handed to the daemon as
	// NOBODY_HOME so that both always agree on it.
	home       string
	projectDir string
	// args are added to the command line (-claude, -hook, -start...); a flag
	// given twice keeps its last value.
	args    []string
	origins []string
	output  io.Writer
	timeout time.Duration
	// prefix is the path under which the control room reaches the daemon
	// (/p/<id>), removed by the proxy.
	prefix string
}

// startProcess starts a project daemon and waits until it answers. When ctx
// ends, the daemon is asked to stop (SIGTERM), then killed if it takes too
// long.
func startProcess(ctx context.Context, cfg processConfig) (*process, error) {
	socket, target, err := openSocket(ctx)
	if err != nil {
		return nil, err
	}
	defer socket.Close() // the child holds its own copy

	p := &process{
		url:    target,
		tail:   &tailBuffer{max: logTailBytes},
		exited: make(chan struct{}),
	}

	p.cmd = command(ctx, cfg, socket)
	p.cmd.Stdout = io.MultiWriter(cfg.output, p.tail)
	p.cmd.Stderr = p.cmd.Stdout

	if p.stdin, err = p.cmd.StdinPipe(); err != nil {
		return nil, err
	}

	if err := p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the project daemon: %w", err)
	}

	go func() {
		p.err = p.cmd.Wait()
		close(p.exited)
	}()

	p.proxy = newProxy(target, cfg.prefix)

	if err := p.waitHealthy(ctx, cfg.timeout); err != nil {
		p.stop()

		return nil, err
	}

	return p, nil
}

// openSocket opens a listening socket on a free local port and returns it as
// a file to hand over to the child, with the URL it will answer on. The
// launcher's own copy is closed: only the child listens.
func openSocket(ctx context.Context) (*os.File, *url.URL, error) {
	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("open a port for the project daemon: %w", err)
	}
	defer listener.Close() // the child gets its own copy of the socket

	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		return nil, nil, errors.New("open a port for the project daemon: not a TCP socket")
	}

	socket, err := tcp.File()
	if err != nil {
		return nil, nil, fmt.Errorf("hand the port over to the project daemon: %w", err)
	}

	return socket, &url.URL{Scheme: "http", Host: listener.Addr().String()}, nil
}

// command is the project daemon's command line: `nobodyd project` on the
// handed-over socket (file descriptor 3), with the launcher's NOBODY_HOME.
func command(ctx context.Context, cfg processConfig, socket *os.File) *exec.Cmd {
	args := append([]string{
		"project", "-project", cfg.projectDir, "-listen-fd", "3", "-origins", strings.Join(cfg.origins, ","),
	}, cfg.args...)

	cmd := exec.CommandContext(ctx, cfg.executable, args...) //nolint:gosec // our own binary, with arguments we build
	cmd.ExtraFiles = []*os.File{socket}

	cmd.Env = append(os.Environ(), "NOBODY_HOME="+cfg.home)

	// When ctx ends: stop as on a normal stop, not a kill.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopTimeout

	return cmd
}

// waitHealthy polls the daemon's /healthz until it answers, it exits, or the
// timeout runs out.
func (p *process) waitHealthy(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()

	for {
		if p.healthy(ctx) {
			return nil
		}

		select {
		case <-p.exited:
			return p.exitError()
		case <-ctx.Done():
			return fmt.Errorf("the project daemon did not answer within %s", timeout)
		case <-ticker.C:
		}
	}
}

func (p *process) healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url.JoinPath("/healthz").String(), http.NoBody)
	if err != nil {
		return false
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	_ = res.Body.Close() // only the status matters

	return res.StatusCode == http.StatusOK
}

// exitError describes how the process ended, with its last log line, which
// usually says why. Call it once exited is closed.
func (p *process) exitError() error {
	reason := "the project daemon stopped"
	if p.err != nil {
		reason += " (" + p.err.Error() + ")"
	}

	if last := p.tail.lastLine(); last != "" {
		reason += ": " + last
	}

	return errors.New(reason)
}

// stop asks the daemon to stop (SIGTERM: it stops its agents and waits for
// its background work), kills it if it takes too long, and waits for it.
func (p *process) stop() {
	select {
	case <-p.exited:
		return
	default:
	}

	_ = p.cmd.Process.Signal(syscall.SIGTERM) // it may have just exited

	select {
	case <-p.exited:
	case <-time.After(stopTimeout):
		_ = p.cmd.Process.Kill() // it may have just exited
		<-p.exited
	}
}

// tailBuffer keeps the last bytes written to it: the end of a project
// daemon's log, to say why it stopped.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf = append(t.buf, b...)
	if extra := len(t.buf) - t.max; extra > 0 {
		t.buf = t.buf[extra:]
	}

	return len(b), nil
}

// lastLine returns the last non-empty line written.
func (t *tailBuffer) lastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
