package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/projectenv"
	"github.com/qwackididuck/nobodyinc/internal/projects"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// shutdownTimeout bounds how long in-flight requests get on shutdown.
const shutdownTimeout = 10 * time.Second

// projectOptions are the flags of `nobodyd project`, the project daemon the
// launcher starts for each open project (ADR-0014). It is not meant to be run
// by hand.
type projectOptions struct {
	projectDir string
	listenFD   int
	start      bool
	claudePath string
	hookPath   string
	origins    string
}

// runProjectCommand implements `nobodyd project`.
func runProjectCommand(logger *slog.Logger, args []string) error {
	var opts projectOptions

	fs := flag.NewFlagSet("project", flag.ContinueOnError)
	fs.StringVar(&opts.projectDir, "project", "", "project directory (holds .nobody/)")
	fs.IntVar(&opts.listenFD, "listen-fd", 0, "inherited file descriptor of the listening socket")
	fs.BoolVar(&opts.start, "start", true, "launch every agent at startup")
	fs.StringVar(&opts.claudePath, "claude", "claude", "claude executable")
	fs.StringVar(&opts.hookPath, "hook", "", "nobody-hook binary (default: next to nobodyd)")
	fs.StringVar(&opts.origins, "origins", "", "browser origins (host patterns) allowed to change things")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if opts.projectDir == "" || opts.listenFD == 0 {
		return errors.New("nobodyd project is started by the launcher: -project and -listen-fd are required")
	}

	return runProject(logger, opts)
}

// runProject runs one project's daemon until the launcher stops it (SIGTERM)
// or goes away (its pipe on our standard input closes): a project daemon
// never outlives its launcher.
func runProject(logger *slog.Logger, opts projectOptions) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin) // returns when the launcher's end closes

		stop()
	}()

	listener, err := net.FileListener(os.NewFile(uintptr(opts.listenFD), "listener"))
	if err != nil {
		return fmt.Errorf("use the inherited listener: %w", err)
	}

	projectDir, err := filepath.Abs(opts.projectDir)
	if err != nil {
		return err
	}

	logger = logger.With("project", projects.ID(projectDir))

	organization, err := loadOrganization(logger, projectDir)
	if err != nil {
		return err
	}

	home, state, unlock, err := lockState(projectDir)
	if err != nil {
		return err
	}
	defer unlock()

	addr := listener.Addr().String()

	daemonOpts, err := daemonOptions(logger, opts, organization, home, state, addr)
	if err != nil {
		return err
	}

	// Before any agent starts: they all get a directory inside it (ADR-0011).
	trustNobodyHome(logger, home)

	d, err := daemon.New(ctx, daemonOpts)
	if err != nil {
		return err
	}
	defer d.Close()

	if opts.start {
		if err := d.Supervisor.StartAll(ctx); err != nil {
			logger.Error("some agents did not start", "err", err)
		}
	}

	return serve(ctx, logger, listener, d.Handler)
}

// lockState returns the user's Nobody Inc directory and the project's state
// directory in it, created if needed and locked: one daemon per project
// (projects.Lock). Call unlock when the daemon stops.
func lockState(projectDir string) (home, state string, unlock func(), err error) { //nolint:nonamedreturns // documents the values
	if home, err = projects.Home(); err != nil {
		return "", "", nil, err
	}

	if state, err = projects.EnsureStateDir(home, projectDir); err != nil {
		return "", "", nil, err
	}

	if unlock, err = projects.Lock(state); err != nil {
		return "", "", nil, err
	}

	return home, state, unlock, nil
}

// loadOrganization reads the project's .nobody/, giving it back its director
// if it lost it, and logs what the human should fix there.
func loadOrganization(logger *slog.Logger, projectDir string) (*org.Org, error) {
	if err := ensureDirector(logger, projectDir); err != nil {
		return nil, err
	}

	organization, err := org.LoadProject(projectDir)
	if err != nil {
		return nil, err
	}

	warnAbout(logger, organization)

	return organization, nil
}

// warnAbout logs what the human should fix in the project's configuration:
// an untrusted project, former channels, MCP servers, skills and commands
// left out.
func warnAbout(logger *slog.Logger, organization *org.Org) {
	warnIfUntrusted(logger, organization.ProjectDir)
	warnIfChannels(logger, organization)

	for _, ignored := range organization.IgnoredMCP {
		logger.Warn("MCP server left out", "server", ignored)
	}

	for _, ignored := range organization.Skills.Ignored {
		logger.Warn("skill left out", "skill", ignored)
	}

	for _, ignored := range organization.Commands.Ignored {
		logger.Warn("command left out", "command", ignored)
	}
}

// ensureDirector gives a configured project its director if it lost it: the
// director is always present (docs/god.md). A project without configuration
// is left to LoadProject, which says so.
func ensureDirector(logger *slog.Logger, projectDir string) error {
	if !org.HasConfig(projectDir) {
		return nil
	}

	god, err := catalog.Director()
	if err != nil {
		return err
	}

	created, err := org.EnsureDirector(projectDir, god.Name, god.Recruit())
	if err != nil {
		return err
	}

	if created {
		logger.Info("the project had no director: created one", "name", god.Name)
	}

	return nil
}

// daemonOptions gathers what the daemon needs besides the organization: the
// user's settings and templates in home, the project's state directory there
// (database, worktrees), and the executables.
func daemonOptions(logger *slog.Logger, opts projectOptions, organization *org.Org,
	home, state, addr string,
) (daemon.Options, error) {
	userSettings, err := settings.Load(home)
	if err != nil {
		return daemon.Options{}, err
	}

	hookPath, err := resolveHookPath(opts.hookPath)
	if err != nil {
		return daemon.Options{}, err
	}

	personalSkills, err := skills.PersonalDir()
	if err != nil {
		return daemon.Options{}, err
	}

	personalCommands, err := commands.PersonalDir()
	if err != nil {
		return daemon.Options{}, err
	}

	return daemon.Options{
		Version:      version,
		Logger:       logger,
		Org:          org.NewLive(organization),
		CatalogDir:   filepath.Join(home, "catalog"),
		Settings:     userSettings,
		SettingsHome: home,
		StateDir:     state,
		ListenAddr:   addr,
		StartAgents:  opts.start,
		DBPath:       filepath.Join(state, "nobody.db"),
		WorktreesDir: filepath.Join(state, "worktrees"),
		MCPConfigDir: filepath.Join(state, "mcp"),
		EnvFile:      filepath.Join(state, projectenv.FileName),
		// ADR-0011, ADR-0017: personal skills and commands, linked for the
		// agents.
		SkillsStagingDir:    filepath.Join(state, "skills"),
		PersonalSkillsDir:   personalSkills,
		PersonalCommandsDir: personalCommands,
		ClaudePath:          opts.claudePath,
		HookPath:            hookPath,
		DaemonURL:           "http://" + addr,
		UserClaudeMD:        userClaudeMD(),
		Origins:             splitOrigins(opts.origins),
	}, nil
}

// splitOrigins turns "a,b" into its host patterns; "" is none.
func splitOrigins(origins string) []string {
	if origins == "" {
		return nil
	}

	return strings.Split(origins, ",")
}

// resolveHookPath finds nobody-hook: the given path, or the binary installed
// next to nobodyd (task build puts both in bin/).
func resolveHookPath(path string) (string, error) {
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}

		path = filepath.Join(filepath.Dir(exe), "nobody-hook")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(abs); err != nil {
		return "", errors.New("nobody-hook not found at " + abs + ": build it with `task build` or pass -hook")
	}

	return abs, nil
}

// serve runs the HTTP server on listener until ctx is canceled, then shuts
// it down.
func serve(ctx context.Context, logger *slog.Logger, listener net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("nobodyd listening", "addr", listener.Addr().String(), "version", version)

		errCh <- srv.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		return nil
	case <-ctx.Done():
	}

	// ctx is already canceled here: shut down with a fresh deadline.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

// warnIfChannels tells that the "channels:" of an org.yaml written before
// ADR-0009 are ignored: any team may now start a conversation with any
// other, unless it blocks it.
func warnIfChannels(logger *slog.Logger, o *org.Org) {
	if o.IgnoredChannels == 0 {
		return
	}

	logger.Warn("org.yaml still declares channels: they are ignored (flows are open by default, restrict them "+
		"with a team's block list) and will be dropped the next time org.yaml is written",
		"channels", o.IgnoredChannels, "see", "docs/adr/0009-open-flows-with-a-block-list.md")
}
