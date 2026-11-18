package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/qwackididuck/nobodyinc/internal/launcher"
	"github.com/qwackididuck/nobodyinc/internal/projects"
	"github.com/qwackididuck/nobodyinc/internal/webui"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

// Where the app's launcher listens: a fixed port keeps the web view's storage
// (appearance, open panels) from one launch to the next, and is not the
// development launcher's (7420). Another port is used if it is taken.
const (
	preferredAddr = "127.0.0.1:7421"
	fallbackAddr  = "127.0.0.1:0"
)

// uniqueID identifies the app for its single instance.
const uniqueID = "io.github.qwackididuck.nobodyinc"

// desktop is the running app: the launcher, the Wails application and its
// windows, by the id each window holds its project with.
type desktop struct {
	app     *application.App
	launch  *launcher.Launcher
	baseURL string
	logger  *slog.Logger

	mu      sync.Mutex
	windows map[string]*application.WebviewWindow
}

// options are the app's command-line flags, for development: an app started
// from the Finder gets none.
type options struct {
	// claude replaces the claude found in the login shell's PATH.
	claude string
}

// run starts the launcher and the app, and returns when the app quits, once
// every project daemon has stopped.
func run(opts options) error {
	home, err := projects.Home()
	if err != nil {
		return err
	}

	logs, err := openLog(home)
	if err != nil {
		return err
	}
	defer logs.Close()

	logger := slog.New(slog.NewJSONHandler(logs, nil))
	slog.SetDefault(logger)

	claude := useLoginShellPath(logger)
	if opts.claude != "" {
		claude = opts.claude // e.g. test/fakeclaude, to try the app for free
	}

	binaries, err := siblingBinaries()
	if err != nil {
		return err
	}

	listener, err := listen(context.Background())
	if err != nil {
		return err
	}

	d := &desktop{baseURL: "http://" + listener.Addr().String(), logger: logger, windows: map[string]*application.WebviewWindow{}}

	d.launch = launcher.New(launcher.Options{
		Version:     version,
		Logger:      logger,
		Home:        home,
		Executable:  binaries.nobodyd,
		ProjectArgs: []string{"-claude", claude, "-hook", binaries.hook},
		Output:      logs,
		UI:          webui.Handler(),
		OnHeld:      d.focus,
		OpenWindow:  d.openWindow,
	})

	srv := &http.Server{Handler: d.launch.Handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("launcher server stopped", "err", err)
		}
	}()

	d.app = application.New(application.Options{
		Name:        "Nobody Inc",
		Description: "Teams of Claude Code agents",
		Logger:      logger,
		Assets:      application.AssetOptions{Handler: http.NotFoundHandler()}, // windows load the launcher's URL
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueID,
			// Launching the app again opens a window (Windows, Linux).
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { d.newWindow("") },
		},
		// Every project daemon stops before the process ends.
		OnShutdown: func() {
			_ = srv.Close() // only the listener is left to close
			d.launch.Close()
		},
	})

	d.app.Menu.Set(d.menu())

	if claude == "" {
		d.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			d.app.Dialog.Error().SetTitle("Claude Code not found").
				SetMessage("Nobody Inc runs Claude Code, but the claude command was not found in your shell's PATH. " +
					"Install Claude Code, then open Nobody Inc again. Agents cannot start until then.").Show()
		})
	}

	d.newWindow("")

	return d.app.Run()
}

// menu is the application menu: App (macOS), File (New Window, Close
// Window), Edit (copy and paste in a web view need it on macOS), Window.
func (d *desktop) menu() *application.Menu {
	menu := d.app.NewMenu()
	menu.AddRole(application.AppMenu)

	file := menu.AddSubmenu("File")
	file.Add("New Window").SetAccelerator("CmdOrCtrl+N").OnClick(func(*application.Context) { d.newWindow("") })
	file.AddSeparator()
	file.Add("Close Window").SetAccelerator("CmdOrCtrl+W").OnClick(func(*application.Context) {
		if w := d.app.Window.Current(); w != nil {
			w.Close()
		}
	})

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)

	return menu
}

// newWindow opens a window on a project, or on the projects page ("").
func (d *desktop) newWindow(project string) {
	id := newWindowID()

	query := url.Values{"window": {id}, "desktop": {"1"}}
	if project != "" {
		query.Set("project", project)
	}

	w := d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      id,
		Title:     "Nobody Inc",
		Width:     1440,
		Height:    900,
		MinWidth:  900,
		MinHeight: 600,
		URL:       d.baseURL + "/?" + query.Encode(),
	})

	d.mu.Lock()
	d.windows[id] = w
	d.mu.Unlock()

	d.logger.Info("window opened", "window", id, "project", project)

	// Closing the window lets its project go at once (ADR-0016).
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { d.closed(id) })
}

// closed forgets a window and releases the project it held.
func (d *desktop) closed(id string) {
	d.mu.Lock()
	delete(d.windows, id)
	d.mu.Unlock()

	d.logger.Info("window closed", "window", id)
	d.launch.Release(id)
}

// focus brings the window holding a project to the front, and reports
// whether there was one (launcher.Options.OnHeld).
func (d *desktop) focus(_, holder string) bool {
	d.mu.Lock()
	w, ok := d.windows[holder]
	d.mu.Unlock()

	if !ok {
		return false
	}

	d.logger.Info("project open in another window: bringing it to the front", "window", holder)

	application.InvokeAsync(func() {
		w.Restore()
		w.Show()
		w.Focus()
	})

	return true
}

// openWindow opens a window from the control room (launcher.Options.OpenWindow).
func (d *desktop) openWindow(project string) error {
	application.InvokeAsync(func() { d.newWindow(project) })

	return nil
}

// windowIDBytes is the size of a window id, before hex encoding.
const windowIDBytes = 16

// homeMode is the mode of ~/.nobody when the app creates it.
const homeMode = 0o700

// newWindowID is a random id for a window.
func newWindowID() string {
	b := make([]byte, windowIDBytes)
	_, _ = rand.Read(b) // never fails (crypto/rand)

	return hex.EncodeToString(b)
}

// listen opens the launcher's address: the preferred port, or any free one.
func listen(ctx context.Context) (net.Listener, error) {
	var lc net.ListenConfig

	if l, err := lc.Listen(ctx, "tcp", preferredAddr); err == nil {
		return l, nil
	}

	return lc.Listen(ctx, "tcp", fallbackAddr)
}

// openLog opens the app's log file, ~/.nobody/desktop.log, started afresh at
// every launch: the app has no terminal to log to.
func openLog(home string) (*os.File, error) {
	if err := os.MkdirAll(home, homeMode); err != nil {
		return nil, err
	}

	return os.Create(filepath.Join(home, "desktop.log"))
}

// binaries are the programs shipped next to the app's.
type binaries struct {
	nobodyd, hook string
}

// siblingBinaries finds nobodyd and nobody-hook next to the app's executable.
func siblingBinaries() (binaries, error) {
	executable, err := os.Executable()
	if err != nil {
		return binaries{}, err
	}

	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return binaries{}, err
	}

	dir := filepath.Dir(executable)
	b := binaries{nobodyd: filepath.Join(dir, "nobodyd"), hook: filepath.Join(dir, "nobody-hook")}

	for _, path := range []string{b.nobodyd, b.hook} {
		if _, err := exec.LookPath(path); err != nil {
			return binaries{}, fmt.Errorf("%s is missing next to the app (task desktop:build): %w", filepath.Base(path), err)
		}
	}

	return b, nil
}
