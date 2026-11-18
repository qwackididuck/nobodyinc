// Package daemon assembles Nobody Inc: the store, the post office, the agent
// supervisor, the approval queue and the HTTP API. cmd/nobodyd only adds
// flags and an HTTP listener on top; end-to-end tests build the very same
// daemon with a fake claude.
package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/server"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/skills"
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
	// SettingsHome is where changed settings are saved (~/.nobody); "" keeps
	// changes made through the API in memory.
	SettingsHome string
	// StateDir is this project's directory on this machine
	// (~/.nobody/projects/<id>), ListenAddr where the HTTP server listens and
	// StartAgents whether every agent starts with the daemon. They are only
	// shown in the control room's settings.
	StateDir    string
	ListenAddr  string
	StartAgents bool
	// DBPath is the SQLite file; ":memory:" for a throwaway database.
	DBPath string
	// WorktreesDir holds the teams' worktrees, outside the project
	// (~/.nobody/projects/<id>/worktrees, see workspace.Repo).
	WorktreesDir string
	// MCPConfigDir holds each agent's MCP configuration, readable by the user
	// only (~/.nobody/projects/<id>/mcp, ADR-0010).
	MCPConfigDir string
	// SkillsStagingDir links the user's personal skills for the agents
	// (~/.nobody/projects/<id>/skills, ADR-0011); "" gives them none.
	SkillsStagingDir string
	// PersonalSkillsDir is where the user's personal skills live
	// (skills.PersonalDir); agents may read it, not write it.
	PersonalSkillsDir string
	// ClaudePath and HookPath are the executables run for each agent.
	ClaudePath string
	HookPath   string
	// DaemonURL is how agents reach this daemon, e.g. "http://127.0.0.1:7420".
	DaemonURL string
	// UserClaudeMD is the user's own CLAUDE.md (~/.claude/CLAUDE.md), whose
	// text every agent gets in its system prompt (ADR-0008); "" for none.
	UserClaudeMD string
	// Origins are the browser origins (host patterns) allowed to open
	// terminals and to change things through the API.
	Origins []string
	// Clock drives the scheduled tasks (ADR-0012); nil is the machine's.
	// End-to-end tests give a schedule.ManualClock.
	Clock schedule.Clock
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
	// Events announces changes to the control room (GET /api/events).
	Events *events.Bus
	// Staff changes agents; nil when the organization is fixed.
	Staff *staffing.Service
	// WorkDir is the directory an agent works in: its team's worktree for the
	// active feature, or the project.
	WorkDir func(address string) string

	// background tracks the work started when a feature starts or ends
	// (agents moved, worktrees and branches removed), which Close waits for:
	// it runs git in the project, which must be done before the daemon stops.
	background *sync.WaitGroup
	// stopScheduler stops the scheduled tasks' clock; Close waits for it
	// (schedulerDone) before closing the database.
	stopScheduler context.CancelFunc
	schedulerDone chan struct{}
}

// New builds a daemon. Agents are not started: call Supervisor.StartAll.
// When ctx ends, the event streams end too (an HTTP server's Shutdown would
// wait for them otherwise).
func New(ctx context.Context, opts Options) (*Daemon, error) {
	st, err := store.Open(ctx, opts.DBPath)
	if err != nil {
		return nil, err
	}

	bus := events.NewBus()
	context.AfterFunc(ctx, bus.Close)

	background := &sync.WaitGroup{}

	userSettings := opts.Settings
	if userSettings.ConfigProtection == "" {
		userSettings = settings.Defaults()
	}

	liveSettings := settings.NewLive(opts.SettingsHome, userSettings)

	statuses := runtime.NewStatuses()
	approvals := approval.NewQueue()
	announce(bus, st, statuses, approvals)

	git := &gitWorkspace{
		repo:             workspace.Repo{Dir: opts.Org.Current().ProjectDir, WorktreesDir: opts.WorktreesDir},
		org:              opts.Org,
		configProtection: func() string { return liveSettings.Current().ConfigProtection },
	}
	restoreActiveFeature(ctx, st, git, opts.Logger)

	supervisor := &runtime.Supervisor{
		Org:      opts.Org,
		Statuses: statuses,
		Logger:   opts.Logger,
		NewCommand: runtime.Launch{
			ClaudePath:   opts.ClaudePath,
			HookPath:     opts.HookPath,
			DaemonURL:    opts.DaemonURL,
			WorkDir:      git.workDir,
			MCPConfigDir: opts.MCPConfigDir,
			// Personal skills reach agents through the staging directory.
			SkillsStagingDir:  opts.SkillsStagingDir,
			PersonalSkillsDir: opts.PersonalSkillsDir,
			UserInstructions: func() string {
				return readUserInstructions(opts.Logger, opts.UserClaudeMD)
			},
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
		bus.Publish(events.Event{Type: events.MessageSent, Data: m})
		staff.onSent(context.WithoutCancel(ctx), m)

		if forker.ShouldAnswer(m) {
			go forker.Answer(context.WithoutCancel(ctx), m)

			return
		}

		supervisor.NotifyMail(m.To)
	}

	tickets := &work.Service{Store: announcingWorkStore{Store: st, bus: bus}, Org: opts.Org, Workspace: git}
	// A new feature moves every agent into its team's worktree.
	tickets.OnFeatureStarted = func(f work.Feature) {
		git.activate(f)

		background.Go(func() {
			if err := supervisor.RestartAll(context.WithoutCancel(ctx)); err != nil {
				opts.Logger.Error("restart agents for the new feature", "feature", f.ID, "err", err)
			}
		})
	}

	// The director's feature proposals travel as messages (proposals.go).
	featureProposals := &proposals{messages: messages, logger: opts.Logger}
	tickets.OnFeatureProposed = featureProposals.onProposed
	tickets.OnFeatureDecided = featureProposals.onDecided

	tickets.OnFeatureFinished = onFeatureFinished(ctx, opts, git, supervisor, featureProposals, background)
	tickets.OnTicketDeleted = onTicketDeleted(ctx, messages, opts.Logger)

	notifier := &ticketNotifier{org: opts.Org, statuses: statuses, messages: messages, tickets: tickets, logger: opts.Logger}

	schedules, err := wireSchedules(ctx, opts, scheduleDeps{
		statuses: statuses, tickets: tickets, messages: messages, store: st, bus: bus,
	}, notifier, supervisor)
	if err != nil {
		return nil, err
	}

	// Agents on leave or dismissed hand their tickets and mail over (staff.go),
	// including what was still waiting when the daemon last stopped.
	staff.tickets = tickets
	supervisor.OnRetired = staff.onRetired
	staff.reconcile(ctx)

	cat := catalog.New(opts.CatalogDir)

	staffService := newStaffing(ctx, opts, staffingDeps{catalog: cat, agents: supervisor, messages: messages, bus: bus, git: git})

	director := &directorOffice{org: opts.Org, staff: staffService, catalog: cat, tickets: tickets, statuses: statuses}

	builtinSkills := discoverBuiltinSkills(ctx, opts)

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
		Events:    bus,
		Settings:  liveSettings,
		// Claude Code's built-in skills, discovered in the background.
		BuiltinSkills: builtinSkills,
		Schedules:     schedules,
		Info: server.DaemonInfo{
			ProjectID:   filepath.Base(opts.StateDir),
			StateDir:    opts.StateDir,
			Address:     opts.ListenAddr,
			ClaudePath:  opts.ClaudePath,
			StartAgents: opts.StartAgents,
		},
		Extra: map[string]http.Handler{
			"/mcp/{address}": officemcp.Handler(officemcp.Office{
				Org: opts.Org, Sender: messages, Tickets: tickets, Director: director, Schedules: schedules,
				Version: opts.Version,
			}),
			"GET /api/agents/{address}/terminal": runtime.TerminalHandler(supervisor, opts.Origins, opts.Logger),
		},
	})

	stopScheduler, schedulerDone := startScheduler(ctx, schedules.scheduler)

	return &Daemon{
		Handler:    handler,
		Store:      st,
		Messages:   messages,
		Tickets:    tickets,
		Statuses:   statuses,
		Approvals:  approvals,
		Supervisor: supervisor,
		Events:     bus,
		Staff:      staffService,
		WorkDir:    git.workDir,
		background: background,

		stopScheduler: stopScheduler,
		schedulerDone: schedulerDone,
	}, nil
}

// wireSchedules builds the scheduled tasks (scheduleoffice.go), which follow
// the tickets being closed and the agents going idle, after the notifier and
// the supervisor have reacted.
func wireSchedules(
	ctx context.Context, opts Options, deps scheduleDeps, notifier *ticketNotifier, supervisor *runtime.Supervisor,
) (*scheduleOffice, error) {
	schedules, err := newScheduleOffice(ctx, opts, deps)
	if err != nil {
		return nil, err
	}

	ctx = context.WithoutCancel(ctx)

	deps.tickets.OnEvent = inTurn(notifier.onEvent, func(e work.Event) { schedules.work.onTicketEvent(ctx, e) })
	deps.statuses.OnIdle = inTurn(supervisor.OnIdle, func(address string) { schedules.work.onIdle(ctx, address) })

	return schedules, nil
}

// inTurn returns a callback calling first, then second: services report to
// a single subscriber.
func inTurn[T any](first, second func(T)) func(T) {
	return func(v T) {
		first(v)
		second(v)
	}
}

// startScheduler runs the scheduled tasks' clock until ctx ends or the
// returned function is called; the channel is closed once it has stopped.
func startScheduler(ctx context.Context, scheduler *schedule.Scheduler) (context.CancelFunc, chan struct{}) {
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	context.AfterFunc(ctx, stop)

	done := make(chan struct{})

	go func() {
		defer close(done)

		scheduler.Run(runCtx)
	}()

	return stop, done
}

// onFeatureFinished sends the team agents back to the project once a feature
// is finished, then removes its worktrees when nobody works in them any more,
// and tells the director (ADR-0007).
func onFeatureFinished(
	ctx context.Context, opts Options, git *gitWorkspace, supervisor *runtime.Supervisor, p *proposals, background *sync.WaitGroup,
) func(work.Feature) {
	return func(f work.Feature) {
		git.deactivate()

		director := ""
		if god := opts.Org.Current().God; god != nil {
			director = god.Name
		}

		ctx := context.WithoutCancel(ctx)
		p.onFinished(ctx, director, f)

		background.Go(func() {
			if err := supervisor.RestartAll(ctx); err != nil {
				opts.Logger.Error("restart agents after the feature", "feature", f.ID, "err", err)
			}

			if err := git.cleanup(ctx, f); err != nil {
				opts.Logger.Error("remove the feature's worktrees", "feature", f.ID, "err", err)
			}
		})
	}
}

// onTicketDeleted tells an agent that the ticket it was working on was
// deleted by the human, with an urgent notice it gets at its next tool use.
func onTicketDeleted(ctx context.Context, messages *messaging.Service, logger *slog.Logger) func(work.Ticket) {
	return func(t work.Ticket) {
		_, err := messages.Send(context.WithoutCancel(ctx), messaging.Draft{
			From: org.SystemAddress, To: t.Assignee, Act: messaging.ActInform, Priority: messaging.Urgent,
			Subject: "Ticket " + t.ID + " deleted",
			Body: "The human deleted the ticket " + t.ID + " (" + t.Title + ") while you were working on it. " +
				"Stop working on it now: do not commit or deliver it, and do not use the ticket tools for it. " +
				"Leave your worktree as it is and wait for your next ticket.",
		})
		if err != nil {
			logger.Error("tell an agent its ticket was deleted", "ticket", t.ID, "agent", t.Assignee, "err", err)
		}
	}
}

// announce publishes on the bus the changes the services report through
// their hooks: agent statuses, deliveries and permission requests. Sent
// messages, tickets, features and the organization are announced where those
// services are built (see New).
func announce(bus *events.Bus, st *store.Store, statuses *runtime.Statuses, approvals *approval.Queue) {
	statuses.OnChange = func(address string, s runtime.Status) {
		bus.Publish(events.Event{Type: events.AgentStatus, Data: events.AgentStatusData{Address: address, Status: string(s)}})
	}
	st.OnDelivered = func(recipient string, ids []string) {
		bus.Publish(events.Event{Type: events.MessagesDelivered, Data: events.DeliveredData{Recipient: recipient, IDs: ids}})
	}
	approvals.OnChange = func() {
		bus.Publish(events.Event{Type: events.ApprovalsChanged, Data: events.Empty{}})
	}
}

// Close stops the scheduled tasks, waits for the feature work still running
// in the background, then stops every agent and closes the database.
func (d *Daemon) Close() error {
	d.stopScheduler()
	<-d.schedulerDone
	d.background.Wait()
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

// staffingDeps is what the staffing service is wired to.
type staffingDeps struct {
	catalog  *catalog.Catalog
	agents   staffing.Applier
	messages *messaging.Service
	bus      *events.Bus
	git      *gitWorkspace
}

// newStaffing returns the service that changes the organization, or nil when
// it is fixed (not an *org.Live, as in some tests). After each change, before
// the agents are brought in line with it: a team created during a feature
// gets its worktree, the control room is told over SSE, and the agents get an
// org-change notice.
func newStaffing(ctx context.Context, opts Options, deps staffingDeps) *staffing.Service {
	live, ok := opts.Org.(*org.Live)
	if !ok {
		return nil
	}

	notices := &orgNotices{messages: deps.messages, logger: opts.Logger}

	return &staffing.Service{
		Org: live, Catalog: deps.catalog, Agents: deps.agents,
		// Claude Code writes its own MCP files, run in the project's main
		// checkout (ADR-0010).
		MCP: mcpservers.Writer{ClaudePath: opts.ClaudePath, ProjectDir: live.Current().ProjectDir},
		OnReload: func(before, after *org.Org) {
			if err := deps.git.prepareNewTeams(context.WithoutCancel(ctx), before, after); err != nil {
				opts.Logger.Error("prepare the worktree of a new team", "err", err)
			}

			deps.bus.Publish(events.Event{Type: events.OrgChanged, Data: events.Empty{}})
			notices.send(context.WithoutCancel(ctx), before, after)
		},
	}
}

// discoverBuiltinSkills returns the list of Claude Code's built-in skills,
// filled in the background: discovering them starts claude, without the
// model, which takes a few seconds (skills.DiscoverBuiltin). The control room
// can refresh it (POST /api/skills/builtin/refresh).
func discoverBuiltinSkills(ctx context.Context, opts Options) *skills.Builtin {
	builtin := skills.NewBuiltin(func(ctx context.Context) ([]string, error) {
		o := opts.Org.Current()

		return skills.DiscoverBuiltin(ctx, skills.Discovery{
			ClaudePath: opts.ClaudePath,
			Args:       runtime.SettingSources,
			Dir:        o.ProjectDir,
			Env:        runtime.CleanEnvironment(os.Environ()),
		}, o.Skills)
	})

	go func() {
		builtin.Refresh(ctx)

		if _, err := builtin.Names(); err != nil {
			opts.Logger.Warn("built-in skills not discovered", "err", err)
		}
	}()

	return builtin
}
