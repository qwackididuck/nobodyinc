// Command nobodyd is the Nobody Inc daemon: it loads a project's
// organization, runs one Claude Code session per agent, keeps the mailboxes,
// and serves the API used by the web UI, by nobody-hook and by the agents'
// MCP clients.
//
// Usage:
//
//	nobodyd init [project-dir]       create <project>/.nobody/ with a first team
//	nobodyd -project <project-dir>   run the organization of a project
//	nobodyd -org nobody.yaml         migrate a former nobody.yaml, then run
//
// The organization lives in <project>/.nobody/ (docs/agents.md); the database
// in ~/.nobody/projects/<id>/ unless -db is given. NOBODY_HOME replaces
// ~/.nobody. The listen address defaults to 127.0.0.1:7420 and can be changed
// with the NOBODY_ADDR environment variable.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

var version = "dev"

// shutdownTimeout bounds how long in-flight requests get on shutdown.
const shutdownTimeout = 10 * time.Second

type options struct {
	projectDir string
	orgPath    string
	dbPath     string
	start      bool
	claudePath string
	hookPath   string
	origins    string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			logger.Error("nobodyd init failed", "err", err)
			os.Exit(1)
		}

		return
	}

	var opts options
	flag.StringVar(&opts.projectDir, "project", ".", "project directory (holds .nobody/)")
	flag.StringVar(&opts.orgPath, "org", "", "former nobody.yaml to migrate into its project's .nobody/")
	flag.StringVar(&opts.dbPath, "db", "", "SQLite database file (default: ~/.nobody/projects/<id>/nobody.db)")
	flag.BoolVar(&opts.start, "start", true, "launch every agent at startup")
	flag.StringVar(&opts.claudePath, "claude", "claude", "claude executable")
	flag.StringVar(&opts.hookPath, "hook", "", "nobody-hook binary (default: next to nobodyd)")
	flag.StringVar(&opts.origins, "origins", "localhost:3000,127.0.0.1:3000",
		"browser origins (host patterns) allowed to open terminals and change things through the API")
	flag.Parse()

	if err := run(logger, opts); err != nil {
		logger.Error("nobodyd stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	projectDir, err := resolveProject(logger, opts)
	if err != nil {
		return err
	}

	if err := ensureDirector(logger, projectDir); err != nil {
		return err
	}

	organization, err := org.LoadProject(projectDir)
	if err != nil {
		return err
	}

	warnAbout(logger, organization)

	addr := listenAddr()

	daemonOpts, err := daemonOptions(logger, opts, organization, addr)
	if err != nil {
		return err
	}

	// Before any agent starts: they all get a directory inside it (ADR-0011).
	if home, err := nobodyHome(); err == nil {
		trustNobodyHome(logger, home)
	}

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

	return serve(ctx, logger, addr, d.Handler)
}

// warnAbout logs what the human should fix in the project's configuration:
// an untrusted project, former channels, MCP servers and skills left out.
func warnAbout(logger *slog.Logger, organization *org.Org) {
	warnIfUntrusted(logger, organization.ProjectDir)
	warnIfChannels(logger, organization)

	for _, ignored := range organization.IgnoredMCP {
		logger.Warn("MCP server left out", "server", ignored)
	}

	for _, ignored := range organization.Skills.Ignored {
		logger.Warn("skill left out", "skill", ignored)
	}
}

// ensureDirector gives a configured project its director if it lost it: the
// director is always present (docs/god.md). A project without configuration
// is left to LoadProject, which says how to create one.
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
// user's settings and templates in ~/.nobody, the project's state directory
// there (database, worktrees), and the executables.
func daemonOptions(logger *slog.Logger, opts options, organization *org.Org, addr string) (daemon.Options, error) {
	home, err := nobodyHome()
	if err != nil {
		return daemon.Options{}, err
	}

	state, err := stateDir(home, organization.ProjectDir)
	if err != nil {
		return daemon.Options{}, err
	}

	dbPath := opts.dbPath
	if dbPath == "" {
		dbPath = filepath.Join(state, "nobody.db")
	}

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
		DBPath:       dbPath,
		WorktreesDir: filepath.Join(state, "worktrees"),
		MCPConfigDir: filepath.Join(state, "mcp"),
		// ADR-0011: personal skills, linked for the agents.
		SkillsStagingDir:  filepath.Join(state, "skills"),
		PersonalSkillsDir: personalSkills,
		ClaudePath:        opts.claudePath,
		HookPath:          hookPath,
		DaemonURL:         "http://" + addr,
		UserClaudeMD:      userClaudeMD(),
		Origins:           strings.Split(opts.origins, ","),
	}, nil
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

// listenAddr is loopback by default: the daemon spawns processes and must not
// be exposed without authentication.
func listenAddr() string {
	if addr := os.Getenv("NOBODY_ADDR"); addr != "" {
		return addr
	}

	return "127.0.0.1:7420"
}

// serve runs the HTTP server until ctx is canceled, then shuts it down.
func serve(ctx context.Context, logger *slog.Logger, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("nobodyd listening", "addr", addr, "version", version)

		errCh <- srv.ListenAndServe()
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
