// Package launcher is what `nobodyd` runs (ADR-0014, docs/projects.md): it
// lets the control room drive every project from one address, and runs one
// project daemon per open project, as a child process.
//
// The launcher listens on 127.0.0.1:7420 and serves:
//
//   - the known projects (GET/POST /api/projects, DELETE
//     /api/projects/{project}), kept in ~/.nobody/projects.yaml;
//   - the folders of the machine, to choose a project (GET /api/fs/dirs);
//   - whether a newer release exists (GET /api/update, POST
//     /api/update/check; update.go, docs/updates.md);
//   - the windows' holds (GET /api/projects/{project}/attach, a WebSocket):
//     a project runs only while a window holds it (leases);
//   - each running project's API, /p/{project}/api/..., proxied to its
//     daemon's /api/... (proxy.go).
//
// A project daemon is the same daemon as before, started as
// `nobodyd project` on a socket the launcher hands over (process.go). Agents
// talk to their project daemon directly; only the control room goes through
// the launcher.
package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/projects"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/server"
	"github.com/qwackididuck/nobodyinc/internal/updates"
)

// Defaults of Options.
const (
	DefaultGrace        = 15 * time.Second
	DefaultPingInterval = 10 * time.Second
	DefaultStartTimeout = 2 * time.Minute
)

var errNotRunning = errors.New("the project is not running: open it in a window")

// Options configure a launcher.
type Options struct {
	Version string
	Logger  *slog.Logger
	// Home is the user's Nobody Inc directory (projects.Home).
	Home string
	// Executable is the nobodyd binary started for each project.
	Executable string
	// ProjectArgs are added to every project daemon's command line, e.g.
	// -claude, -hook, -start=false.
	ProjectArgs []string
	// Origins are the browser origins (host patterns) allowed to change
	// things and to open WebSockets, handed to the project daemons too.
	Origins []string
	// Output receives the project daemons' logs; default os.Stdout.
	Output io.Writer
	// Clock measures the grace period; default the real clock.
	Clock schedule.Clock
	// Grace is how long a project keeps running after its window's
	// connection dropped (DefaultGrace).
	Grace time.Duration
	// PingInterval is how often a window's connection is checked
	// (DefaultPingInterval).
	PingInterval time.Duration
	// StartTimeout bounds a project daemon's start (DefaultStartTimeout).
	StartTimeout time.Duration
	// UI serves the control room at / (webui.Handler, ADR-0016); nil serves
	// nothing there (development: next dev serves it).
	UI http.Handler
	// OnHeld, when set, is called when a window asks for a project another
	// connected window holds, with that window's id: it reports whether it
	// brought that window to the front (the desktop app, ADR-0016).
	OnHeld func(project, holder string) bool
	// OpenWindow, when set, opens a new window of the desktop app, on a
	// project or on the projects page (""): POST /api/windows (windows.go).
	OpenWindow func(project string) error
	// Updates looks for newer releases; default one asks GitHub about this
	// Version (docs/updates.md). Tests give it a fake GitHub.
	Updates *updates.Checker
	// NoUpdateCheck skips the check made when the launcher starts
	// (`check_updates: false` in settings.yaml). The control room can still
	// ask for one.
	NoUpdateCheck bool
}

// Launcher runs the projects' daemons. Build it with New, serve Handler,
// and Close it to stop every project daemon.
type Launcher struct {
	Handler http.Handler

	opts     Options
	updates  *updates.Checker
	registry *projects.Registry
	leases   *leases
	// ctx outlives requests: project daemons start with it, and are asked to
	// stop when it ends (Close).
	ctx    context.Context //nolint:containedctx // the launcher's lifetime, not a request's
	cancel context.CancelFunc

	mu        sync.Mutex
	projects  map[string]*project
	closeOnce sync.Once
}

// New returns a launcher with these options, defaults filled in.
func New(opts Options) *Launcher {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	if opts.Clock == nil {
		opts.Clock = schedule.RealClock{}
	}

	if opts.Grace == 0 {
		opts.Grace = DefaultGrace
	}

	if opts.PingInterval == 0 {
		opts.PingInterval = DefaultPingInterval
	}

	if opts.StartTimeout == 0 {
		opts.StartTimeout = DefaultStartTimeout
	}

	if opts.Updates == nil {
		opts.Updates = updates.New(updates.Options{Current: opts.Version})
	}

	ctx, cancel := context.WithCancel(context.Background())

	l := &Launcher{
		opts:     opts,
		updates:  opts.Updates,
		registry: projects.NewRegistry(opts.Home),
		ctx:      ctx,
		cancel:   cancel,
		projects: make(map[string]*project),
	}
	l.leases = newLeases(opts.Clock, opts.Grace, l.stopUnlessHeld)
	l.Handler = l.routes()

	if !opts.NoUpdateCheck {
		l.checkAtStart()
	}

	return l
}

// slowStop is how long stopping a project daemon takes before it is worth a
// warning: quitting the desktop app waits for it, on the app's main thread,
// with its window frozen (docs/desktop.md).
const slowStop = time.Second

// Close stops every project daemon, and logs how long it took. It is safe to
// call more than once: the desktop app stops the daemons before it quits, and
// its shutdown hook calls it again (a second call waits for the first).
func (l *Launcher) Close() {
	l.closeOnce.Do(l.closeAll)
}

// Release lets go at once of the project a window holds, if any: the
// desktop app closed the window (ADR-0016). Its daemon stops, without the
// grace period a dropped connection gets.
func (l *Launcher) Release(window string) {
	if l.leases.releaseWindow(window) {
		l.opts.Logger.Info("window closed: project released", "window", window)
	}
}

// closeAll is what Close does, once.
func (l *Launcher) closeAll() {
	begun := time.Now()

	l.cancel()

	l.mu.Lock()
	all := make([]*project, 0, len(l.projects))

	for _, p := range l.projects {
		all = append(all, p)
	}
	l.mu.Unlock()

	var wg sync.WaitGroup

	for _, p := range all {
		wg.Go(func() {
			stopping := time.Now()

			p.stop()

			if took := time.Since(stopping); took > slowStop {
				l.opts.Logger.Warn("a project daemon was slow to stop", "project", p.id, "took", took.Round(time.Millisecond))
			}
		})
	}

	wg.Wait()
	l.opts.Logger.Info("project daemons stopped", "projects", len(all), "took", time.Since(begun).Round(time.Millisecond))
}

func (l *Launcher) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", l.health)
	mux.HandleFunc("GET /api/projects", l.listProjects)
	mux.HandleFunc("POST /api/projects", l.openProject)
	mux.HandleFunc("DELETE /api/projects/{project}", l.deleteProject)
	mux.HandleFunc("GET /api/projects/{project}/attach", l.attach)
	mux.HandleFunc("GET /api/fs/dirs", l.listDirs)
	mux.HandleFunc("GET /api/update", l.getUpdate)
	mux.HandleFunc("POST /api/update/check", l.checkUpdate)
	mux.HandleFunc("/p/{project}/", l.proxyToProject)

	if l.opts.OpenWindow != nil {
		mux.HandleFunc("POST /api/windows", l.openWindow)
	}

	if l.opts.UI != nil {
		// "/" without a method: "GET /" would conflict with "/p/{project}/".
		mux.Handle("/", readOnly(l.opts.UI))
	}

	return server.GuardCrossSite(l.opts.Origins, mux)
}

// readOnly lets only GET and HEAD requests through to h.
func readOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))

			return
		}

		h.ServeHTTP(w, r)
	})
}

// start starts the project's daemon in the background, unless it runs
// already. The daemon lives as long as the launcher, not as the request
// that asked for it.
func (l *Launcher) start(p *project) {
	go p.ensureRunning(l.ctx, l.processConfig(p.id, p.dir), l.opts.Logger)
}

// project returns the running state of a known project, created when first
// asked for.
func (l *Launcher) project(entry projects.Entry) *project {
	l.mu.Lock()
	defer l.mu.Unlock()

	p, ok := l.projects[entry.ID]
	if !ok {
		p = &project{id: entry.ID, dir: entry.Path, subscribers: make(map[chan stateEvent]struct{})}
		l.projects[entry.ID] = p
	}

	return p
}

// existingProject returns the running state of a project, nil if it never
// ran.
func (l *Launcher) existingProject(id string) *project {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.projects[id]
}

// runningProcess returns the daemon of a running project, nil if it is not
// running.
func (l *Launcher) runningProcess(id string) *process {
	p := l.existingProject(id)
	if p == nil {
		return nil
	}

	return p.running()
}

// processConfig is how to start the daemon of the project in dir.
func (l *Launcher) processConfig(id, dir string, extra ...string) processConfig {
	return processConfig{
		executable: l.opts.Executable,
		home:       l.opts.Home,
		projectDir: dir,
		args:       append(append([]string{}, l.opts.ProjectArgs...), extra...),
		origins:    l.opts.Origins,
		output:     l.opts.Output,
		timeout:    l.opts.StartTimeout,
		prefix:     "/p/" + id,
	}
}

// stopUnlessHeld stops a project whose hold ended, unless a window acquired
// it again meanwhile. It is called when a hold expires or is released.
func (l *Launcher) stopUnlessHeld(id string) {
	p := l.existingProject(id)
	if p == nil {
		return
	}

	go func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		if l.leases.held(id) {
			return
		}

		l.opts.Logger.Info("no window holds the project: stopping it", "project", id)
		p.stopLocked()
	}()
}

func (l *Launcher) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, server.Health{Status: "ok", Version: l.opts.Version})
}

// Project daemon states, as the API shows them.
const (
	stateStopped  = "stopped"
	stateStarting = "starting"
	stateRunning  = "running"
	stateStopping = "stopping"
)

// subscriberBuffer is how many state changes a slow window may fall behind
// before it misses some (it then catches up with the next one).
const subscriberBuffer = 8

// stateEvent tells the windows of a project how its daemon is doing.
type stateEvent struct {
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
}

// project is the launcher's view of one project's daemon.
type project struct {
	id, dir string

	// mu serializes starting and stopping the daemon.
	mu sync.Mutex

	stateMu     sync.Mutex
	state       string
	lastError   string
	proc        *process
	subscribers map[chan stateEvent]struct{}
}

// ensureRunning starts the project's daemon unless it runs already.
func (p *project) ensureRunning(ctx context.Context, cfg processConfig, logger *slog.Logger) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.current().State == stateRunning {
		return
	}

	p.setState(stateStarting, "", nil)
	logger.Info("starting the project daemon", "project", p.id)

	proc, err := startProcess(ctx, cfg)
	if err != nil {
		logger.Error("the project daemon did not start", "project", p.id, "err", err)
		p.setState(stateStopped, err.Error(), nil)

		return
	}

	p.setState(stateRunning, "", proc)

	go p.watch(proc, logger)
}

// watch notices a daemon that stops on its own (a crash, a lost lock).
func (p *project) watch(proc *process, logger *slog.Logger) {
	<-proc.exited

	p.stateMu.Lock()
	unexpected := p.proc == proc && p.state == stateRunning
	p.stateMu.Unlock()

	if unexpected {
		err := proc.exitError()
		logger.Error("the project daemon stopped on its own", "project", p.id, "err", err)
		p.setState(stateStopped, err.Error(), nil)
	}
}

// running returns the daemon if it runs, nil otherwise.
func (p *project) running() *process {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	if p.state != stateRunning {
		return nil
	}

	return p.proc
}

// stop stops the project's daemon, if it runs.
func (p *project) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopLocked()
}

// stopLocked is stop with p.mu held.
func (p *project) stopLocked() {
	p.stateMu.Lock()
	proc := p.proc
	p.stateMu.Unlock()

	if proc == nil {
		return
	}

	p.setState(stateStopping, "", proc)
	proc.stop()
	p.setState(stateStopped, "", nil)
}

// current returns the daemon's state.
func (p *project) current() stateEvent {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	return p.currentLocked()
}

func (p *project) currentLocked() stateEvent {
	if p.state == "" {
		return stateEvent{State: stateStopped, Error: p.lastError}
	}

	return stateEvent{State: p.state, Error: p.lastError}
}

// setState changes the daemon's state and tells the subscribed windows.
func (p *project) setState(state, errText string, proc *process) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	p.state, p.lastError, p.proc = state, errText, proc
	event := p.currentLocked()

	for ch := range p.subscribers {
		select {
		case ch <- event:
		default: // a slow window catches up with the next event
		}
	}
}

// subscribe returns the current state, then every change, until
// unsubscribe.
func (p *project) subscribe() (stateEvent, <-chan stateEvent, func()) {
	ch := make(chan stateEvent, subscriberBuffer)

	p.stateMu.Lock()
	p.subscribers[ch] = struct{}{}
	current := p.currentLocked()
	p.stateMu.Unlock()

	return current, ch, func() {
		p.stateMu.Lock()
		delete(p.subscribers, ch)
		p.stateMu.Unlock()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write a response: the client may be gone", "err", err)
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorBody{Error: err.Error()})
}
