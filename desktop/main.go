// Command nobody-desktop is the Nobody Inc desktop app (ADR-0016,
// docs/desktop.md): the launcher (internal/launcher) runs inside the app's
// process, and every window is a native web view on the control room it
// serves, one project per window.
//
//   - The first window shows the projects page; Cmd/Ctrl+N or File > New
//     Window opens another, and so does launching the app again on Windows
//     and Linux (one instance: the second process hands over and exits).
//   - Closing a window releases its project at once: its daemon stops.
//   - Opening a project another window holds brings that window to the front.
//   - Closing the last window quits the app, which stops every project: its
//     windows go at once, and a "closing" window shows if stopping takes a
//     moment (quit.go).
//
// The project daemons are the nobodyd binary shipped next to this one, with
// nobody-hook for the agents' hooks (in the macOS app bundle, both are in
// Contents/MacOS/).
//
// It lives in its own Go module so that Wails, cgo and the web view libraries
// never reach the main module.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	var opts options

	flag.StringVar(&opts.claude, "claude", "", "claude executable (default: found in the login shell's PATH)")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "nobody-desktop:", err)
		slog.Error("nobody-desktop stopped", "err", err)
		os.Exit(1)
	}
}
