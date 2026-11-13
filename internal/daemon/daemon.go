// Package daemon assembles Nobody Inc: the store, the post office, the agent
// supervisor, the approval queue and the HTTP API. cmd/nobodyd only adds
// flags and an HTTP listener on top; end-to-end tests build the very same
// daemon with a fake claude.
package daemon

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/server"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/staffing"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
	"github.com/qwackididuck/nobodyinc/internal/workspace"
)

// Options configure a daemon.
type Options struct {
	Version string
	Logger  *slog.Logger
	// Org is the organization. With an *org.Live, agents can be recruited,
	// edited, put on leave and dismissed through the API; a fixed *org.Org
	// disables that.
	Org org.Source
	// CatalogDir holds the user's agent templates (~/.nobody/catalog).
	CatalogDir string
	// Settings are the user's global settings (~/.nobody/settings.yaml); the
	// zero value means the defaults.
	Settings settings.Settings
	// DBPath is the SQLite file; ":memory:" for a throwaway database.
	DBPath string
	// WorktreesDir holds the teams' worktrees, outside the project
	// (~/.nobody/projects/<id>/worktrees, see workspace.Repo).
	WorktreesDir string
	// ClaudePath and HookPath are the executables run for each agent.
	ClaudePath string
	HookPath   string
	// DaemonURL is how agents reach this daemon, e.g. "http://127.0.0.1:7420".
	DaemonURL string
	// Origins are the browser origins (host patterns) allowed to open
	// terminals and to change things through the API.
	Origins []string
}

// Daemon is a fully wired Nobody Inc.
type Daemon struct {
	Handler    http.Handler
	Store      *store.Store
	Messages   *messaging.Service
	Tickets    *work.Service
	Statuses   *runtime.Statuses
	Approvals  *approval.Queue
	Supervisor *runtime.Supervisor
	// Staff changes agents; nil when the organization is fixed.
	Staff *staffing.Service
	// WorkDir is the directory an agent works in: its team's worktree for the
	// active feature, or the project.
	WorkDir func(address string) string
}

// New builds a daemon. Agents are not started: call Supervisor.StartAll.
func New(ctx context.Context, opts Options) (*Daemon, error) {
	st, err := store.Open(ctx, opts.DBPath)
	if err != nil {
		return nil, err
	}

	statuses := runtime.NewStatuses()
	approvals := approval.NewQueue()
	git := &gitWorkspace{
		repo:             workspace.Repo{Dir: opts.Org.Current().ProjectDir, WorktreesDir: opts.WorktreesDir},
		org:              opts.Org,
		configProtection: opts.Settings.ConfigProtection,
	}
	restoreActiveFeature(ctx, st, git, opts.Logger)

	supervisor := &runtime.Supervisor{
		Org:      opts.Org,
		Statuses: statuses,
		Logger:   opts.Logger,
		NewCommand: runtime.Launch{
			ClaudePath: opts.ClaudePath,
			HookPath:   opts.HookPath,
			DaemonURL:  opts.DaemonURL,
			WorkDir:    git.workDir,
		}.Command,
		HasMail: func(ctx context.Context, address string) (bool, error) {
			return hasMail(ctx, st, address)
		},
	}

	messages := &messaging.Service{
		Store:  st,
		Policy: messaging.Policy{Org: opts.Org},
		PickMember: func(team string) string {
			return pickMember(opts.Org.Current(), statuses, team, "")
		},
	}
	staff := &staff{org: opts.Org, store: st, messages: messages, logger: opts.Logger}
	git.onConfigDelivered = staff.onConfigDelivered
	forker := &runtime.Forker{
		Org:           opts.Org,
		Statuses:      statuses,
		Logger:        opts.Logger,
		ClaudePath:    opts.ClaudePath,
		WorkDir:       git.workDir,
		Send:          messages.Send,
		MarkDelivered: st.MarkDelivered,
		Requeue:       st.Requeue,
	}

	// What happens when mail is sent (see docs/hooks.md):
	//   - an urgent question to a busy agent is answered by a fork of its
	//     session, without disturbing it;
	//   - otherwise an idle recipient is woken up, and a busy one gets the mail
	//     from its hooks.
	// An agent going idle also checks for mail that arrived as its turn ended.
	messages.OnSent = func(m messaging.Message) {
		staff.onSent(context.WithoutCancel(ctx), m)

		if forker.ShouldAnswer(m) {
			go forker.Answer(context.WithoutCancel(ctx), m)

			return
		}

		supervisor.NotifyMail(m.To)
	}
	statuses.OnIdle = supervisor.OnIdle

	tickets := &work.Service{Store: st, Org: opts.Org, Workspace: git}
	// A new feature moves every agent into its team's worktree.
	tickets.OnFeatureStarted = func(f work.Feature) {
		git.activate(f)

		go func() {
			if err := supervisor.RestartAll(context.WithoutCancel(ctx)); err != nil {
				opts.Logger.Error("restart agents for the new feature", "feature", f.ID, "err", err)
			}
		}()
	}

	notifier := &ticketNotifier{org: opts.Org, statuses: statuses, messages: messages, tickets: tickets, logger: opts.Logger}
	tickets.OnEvent = notifier.onEvent

	// Agents on leave or dismissed hand their tickets and mail over (staff.go),
	// including what was still waiting when the daemon last stopped.
	staff.tickets = tickets
	supervisor.OnRetired = staff.onRetired
	staff.reconcile(ctx)

	cat := catalog.New(opts.CatalogDir)

	var staffService *staffing.Service
	if live, ok := opts.Org.(*org.Live); ok {
		staffService = &staffing.Service{Org: live, Catalog: cat, Agents: supervisor}
	}

	director := &directorOffice{org: opts.Org, staff: staffService, catalog: cat, tickets: tickets, statuses: statuses}

	handler := server.New(server.Deps{
		Version:   opts.Version,
		Logger:    opts.Logger,
		Org:       opts.Org,
		Store:     st,
		Messages:  messages,
		Tickets:   tickets,
		Statuses:  statuses,
		Approvals: approvals,
		Agents:    supervisor,
		Staff:     staffService,
		Catalog:   cat,
		Origins:   opts.Origins,
		Extra: map[string]http.Handler{
			"/mcp/{address}": officemcp.Handler(officemcp.Office{
				Org: opts.Org, Sender: messages, Tickets: tickets, Director: director, Version: opts.Version,
			}),
			"GET /api/agents/{address}/terminal": runtime.TerminalHandler(supervisor, opts.Origins, opts.Logger),
		},
	})

	return &Daemon{
		Handler:    handler,
		Store:      st,
		Messages:   messages,
		Tickets:    tickets,
		Statuses:   statuses,
		Approvals:  approvals,
		Supervisor: supervisor,
		Staff:      staffService,
		WorkDir:    git.workDir,
	}, nil
}

// Close stops every agent and closes the database.
func (d *Daemon) Close() error {
	d.Supervisor.StopAll()

	return d.Store.Close()
}

// restoreActiveFeature puts agents back into the worktrees of the feature that
// was active when the daemon last stopped.
func restoreActiveFeature(ctx context.Context, st *store.Store, git *gitWorkspace, logger *slog.Logger) {
	features, err := st.ListFeatures(ctx)
	if err != nil {
		logger.Error("list features", "err", err)

		return
	}

	for _, f := range features {
		if !f.Active {
			continue
		}

		if _, err := git.PrepareFeature(ctx, f); err != nil {
			logger.Error("prepare the active feature, agents stay in the project directory", "feature", f.ID, "err", err)

			return
		}

		git.activate(f)
	}
}

// hasMail reports whether an agent has undelivered mail in any tray.
func hasMail(ctx context.Context, st *store.Store, address string) (bool, error) {
	for _, p := range []messaging.Priority{messaging.Urgent, messaging.Normal} {
		pending, err := st.PendingMessages(ctx, address, p)
		if err != nil {
			return false, err
		}

		if len(pending) > 0 {
			return true, nil
		}
	}

	return false, nil
}
