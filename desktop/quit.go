package main

import (
	"maps"
	"slices"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Quitting (docs/desktop.md). Wails runs its OnShutdown on the app's main
// thread, before it closes the windows: stopping the project daemons there
// freezes the window until they have stopped, however long that takes, with
// no sign of what is happening. So the app answers "not yet" to the first
// request to quit (ShouldQuit: the last window closed, Cmd+Q, the Quit menu),
// closes its windows at once, stops the daemons in the background while the
// interface stays alive, shows a "closing" window if that takes more than a
// moment, then quits for good.

// quitState says where quitting stands.
type quitState int

const (
	// notQuitting: the app runs.
	notQuitting quitState = iota
	// stopping: the windows are going and the project daemons are being
	// stopped.
	stopping
	// stopped: the daemons are stopped, the app may quit.
	stopped
)

// splashDelay is how long stopping takes before the closing window shows:
// usually a few milliseconds, and a window flashing by would be worse than
// none.
const splashDelay = 400 * time.Millisecond

// shouldQuit is asked each time the app is about to terminate (Options.ShouldQuit,
// on the main thread). Only the first request starts stopping; it and those
// during it answer no; once the daemons are stopped, the app quits.
func (d *desktop) shouldQuit() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch d.quit {
	case stopped:
		return true
	case stopping:
		return false
	case notQuitting:
	}

	d.quit = stopping

	go d.stopThenQuit()

	return false
}

// stopThenQuit closes the windows, stops every project daemon, and quits.
func (d *desktop) stopThenQuit() {
	begun := time.Now()

	d.logger.Info("quitting: closing the windows and stopping the project daemons")

	// Closing a window releases its project (closed). Done before the
	// launcher's server closes, so that no window shows an error meanwhile.
	application.InvokeSync(d.closeWindows)

	closing := time.AfterFunc(splashDelay, func() { application.InvokeAsync(d.showClosing) })

	_ = d.srv.Close() // only the listener is left to close
	d.launch.Close()
	closing.Stop()

	d.mu.Lock()
	d.quit = stopped
	d.mu.Unlock()

	d.logger.Info("quitting: the project daemons stopped", "after", time.Since(begun).Round(time.Millisecond))

	d.app.Quit()
}

// closeWindows closes every window of a project (on the main thread).
func (d *desktop) closeWindows() {
	d.mu.Lock()
	open := slices.Collect(maps.Values(d.windows))
	d.mu.Unlock()

	for _, w := range open {
		w.Close()
	}
}

// The closing window: small, frameless, centered, over everything.
const (
	closingWidth  = 360
	closingHeight = 150
)

// showClosing opens the closing window (on the main thread), unless the
// daemons stopped meanwhile.
func (d *desktop) showClosing() {
	d.mu.Lock()
	over := d.quit == stopped
	d.mu.Unlock()

	if over {
		return
	}

	d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "closing",
		Title:            "Nobody Inc",
		Width:            closingWidth,
		Height:           closingHeight,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		BackgroundColour: closingBackground,
		HTML:             closingHTML,
	})

	d.logger.Info("quitting: stopping takes a while, the closing window is shown")
}
