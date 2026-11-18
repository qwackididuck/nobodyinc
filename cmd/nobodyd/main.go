// Command nobodyd is the Nobody Inc daemon (ADR-0014, docs/projects.md).
//
// Run without arguments, it is the launcher: it serves the control room
// (when built with its export, ADR-0016) and its projects (create, open,
// delete), and runs one project daemon per open project, as a child
// process, for as long as a window holds the project.
//
// Usage:
//
//	nobodyd [-claude path] [-hook path] [-start=false] [-origins patterns]
//
// A project daemon (`nobodyd project ...`) loads a project's organization,
// runs one Claude Code session per agent, keeps the mailboxes, and serves
// the API used by the control room (through the launcher), by nobody-hook
// and by the agents' MCP clients. Only the launcher starts it.
//
// The organization lives in <project>/.nobody/ (docs/agents.md); the
// project's state in ~/.nobody/projects/<id>/, the known projects in
// ~/.nobody/projects.yaml. NOBODY_HOME replaces ~/.nobody. The launcher
// listens on 127.0.0.1:7420, or on the NOBODY_ADDR environment variable.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/qwackididuck/nobodyinc/internal/launcher"
	"github.com/qwackididuck/nobodyinc/internal/projects"
	"github.com/qwackididuck/nobodyinc/internal/webui"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var err error
	if len(os.Args) > 1 && os.Args[1] == "project" {
		err = runProjectCommand(logger, os.Args[2:])
	} else {
		err = runLauncher(logger)
	}

	if err != nil {
		logger.Error("nobodyd stopped", "err", err)
		os.Exit(1)
	}
}

// runLauncher implements `nobodyd`: the launcher, until SIGINT or SIGTERM,
// which stop every project daemon.
func runLauncher(logger *slog.Logger) error {
	start := flag.Bool("start", true, "launch every agent when a project opens (false: try the control room for free)")
	claudePath := flag.String("claude", "claude", "claude executable, for every project")
	hookPath := flag.String("hook", "", "nobody-hook binary (default: next to nobodyd)")
	origins := flag.String("origins", "localhost:3000,127.0.0.1:3000",
		"browser origins (host patterns) allowed to open terminals and change things through the API")

	flag.Parse()

	if flag.NArg() > 0 {
		return errors.New("unexpected arguments: " + strings.Join(flag.Args(), " ") +
			" (projects are created and opened from the control room)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	home, err := projects.Home()
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	// Checked here rather than when the first project opens.
	hook, err := resolveHookPath(*hookPath)
	if err != nil {
		return err
	}

	addr := listenAddr()

	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	l := launcher.New(launcher.Options{
		Version:    version,
		Logger:     logger,
		Home:       home,
		Executable: executable,
		ProjectArgs: []string{
			"-claude", *claudePath, "-hook", hook, "-start=" + strconv.FormatBool(*start),
		},
		Origins: splitOrigins(*origins),
		// The control room, when the binary was built with its export.
		UI: webui.Handler(),
	})
	defer l.Close()

	return serve(ctx, logger, listener, l.Handler)
}

// listenAddr is loopback by default: the daemon spawns processes and must not
// be exposed without authentication.
func listenAddr() string {
	if addr := os.Getenv("NOBODY_ADDR"); addr != "" {
		return addr
	}

	return "127.0.0.1:7420"
}
