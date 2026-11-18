package daemon

import (
	"context"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// Resuming the agents' sessions when the project opens again (ADR-0015,
// runtime/sessions.go): the supervisor remembers each agent's session in the
// database, learnt from its hooks (Statuses.OnSession), with whether it is in
// the middle of a turn (Statuses.OnChange); StartAll resumes them.

// newLaunch describes how the daemon starts its agents' Claude Code
// sessions.
func newLaunch(opts Options, git *gitWorkspace) runtime.Launch {
	return runtime.Launch{
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
	}
}

// newSupervisor builds the supervisor of the agents. It remembers their
// sessions in the database, from what the hooks tell (their ID, and whether
// the agent is mid-turn), and resumes them when the project opens again.
// The status observer already set (announce) keeps running first.
func newSupervisor(ctx context.Context, opts Options, st *store.Store, statuses *runtime.Statuses, git *gitWorkspace) *runtime.Supervisor {
	launch := newLaunch(opts, git)
	supervisor := &runtime.Supervisor{
		Org:           opts.Org,
		Statuses:      statuses,
		Logger:        opts.Logger,
		NewCommand:    launch.Command,
		ReopenContext: launch.ReopenContext,
		Sessions:      agentSessions{store: st},
		HasMail: func(ctx context.Context, address string) (bool, error) {
			return hasMail(ctx, st, address)
		},
	}

	statuses.OnSession = func(address, sessionID string) {
		supervisor.RememberSession(ctx, address, sessionID)
	}

	announced := statuses.OnChange
	statuses.OnChange = func(address string, s runtime.Status) {
		if announced != nil {
			announced(address, s)
		}

		supervisor.ObserveStatus(ctx, address, s)
	}

	return supervisor
}

// sessionStarted tells the server what an agent's SessionStart changes
// (Supervisor.SessionStarted): a session resumed silently settles idle, or on
// budget hold when a budget holds the agent (ADR-0013), so that its mail
// does not wake it up. Any other session start changes nothing.
func sessionStarted(
	started func(address string) (string, bool), held func(address string) bool,
) func(string) (string, runtime.Status) {
	return func(address string) (string, runtime.Status) {
		reopen, idle := started(address)

		switch {
		case !idle:
			return reopen, ""
		case held(address):
			return reopen, runtime.StatusBudgetHold
		default:
			return reopen, runtime.StatusIdle
		}
	}
}

// agentSessions keeps the agents' sessions in the database
// (runtime.SessionStore).
type agentSessions struct {
	store *store.Store
}

func (a agentSessions) Sessions(ctx context.Context) (map[string]runtime.SavedSession, error) {
	rows, err := a.store.AgentSessions(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]runtime.SavedSession, len(rows))
	for _, r := range rows {
		out[r.Address] = runtime.SavedSession{ID: r.SessionID, WorkDir: r.WorkDir, MidTurn: r.MidTurn}
	}

	return out, nil
}

func (a agentSessions) SaveSession(ctx context.Context, address, sessionID, workDir string) error {
	return a.store.SaveAgentSession(ctx, address, sessionID, workDir)
}

func (a agentSessions) SetMidTurn(ctx context.Context, address string, midTurn bool) error {
	return a.store.SetAgentMidTurn(ctx, address, midTurn)
}

func (a agentSessions) ForgetSession(ctx context.Context, address string) error {
	return a.store.ForgetAgentSession(ctx, address)
}
