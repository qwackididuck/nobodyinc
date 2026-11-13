package runtime

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// mutableOrg is an org.Source whose organization a test can replace, as
// org.Live does after a reload.
type mutableOrg struct {
	mu sync.Mutex
	o  *org.Org
}

func (m *mutableOrg) Current() *org.Org {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.o
}

func (m *mutableOrg) set(o *org.Org) {
	m.mu.Lock()
	m.o = o
	m.mu.Unlock()
}

// launches records every start of an agent and the session it resumed.
type launches struct {
	mu      sync.Mutex
	resumes []string
}

func (l *launches) add(resume string) {
	l.mu.Lock()
	l.resumes = append(l.resumes, resume)
	l.mu.Unlock()
}

func (l *launches) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.resumes...)
}

// newApplySupervisor runs `cat` for agents and records their launches. The
// agent dev1@dev starts idle, in session "s-1".
func newApplySupervisor(t *testing.T) (*Supervisor, *mutableOrg, *launches) {
	t.Helper()

	source := &mutableOrg{o: applyOrg(false)}
	started := &launches{}

	s := &Supervisor{
		Org:      source,
		Statuses: NewStatuses(),
		Logger:   slog.New(slog.DiscardHandler),
		NewCommand: func(ctx context.Context, _ *org.Org, _ org.Member, resume string) (*exec.Cmd, error) {
			started.add(resume)

			return exec.CommandContext(ctx, "cat"), nil
		},
		HasMail: func(context.Context, string) (bool, error) { return false, nil },
	}
	s.Statuses.OnIdle = s.OnIdle
	t.Cleanup(s.StopAll)

	if err := s.Start(context.Background(), "dev1@dev"); err != nil {
		t.Fatal(err)
	}

	s.Statuses.ObserveHook("dev1@dev", hook.Event{Name: hook.EventStop, SessionID: "s-1"}, 0)

	return s, source, started
}

func applyOrg(onLeave bool) *org.Org {
	dev1 := org.Agent{Name: "dev1", Role: "Developer", OnLeave: onLeave}

	return &org.Org{Teams: []org.Team{{ID: "dev", Agents: []org.Agent{dev1}}}}
}

func TestApplyToAnIdleAgentResumesItsSession(t *testing.T) {
	t.Parallel()

	s, _, started := newApplySupervisor(t)
	before, _ := s.Output("dev1@dev")

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	after, running := s.Output("dev1@dev")
	if !running || after == before {
		t.Fatal("the agent was not restarted")
	}

	if got := started.all(); len(got) != 2 || got[1] != "s-1" {
		t.Fatalf("launches = %q, want a fresh start then a resume of s-1", got)
	}
}

func TestApplyWaitsForTheEndOfTheTurn(t *testing.T) {
	t.Parallel()

	s, _, started := newApplySupervisor(t)
	s.Statuses.Set("dev1@dev", StatusWorking)

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	if !s.Pending("dev1@dev") || len(started.all()) != 1 {
		t.Fatal("a working agent must not be restarted in the middle of its turn")
	}

	s.Statuses.ObserveHook("dev1@dev", hook.Event{Name: hook.EventStop, SessionID: "s-1"}, 0) // the turn ends

	eventually(t, func() bool { return len(started.all()) == 2 }, "agent restarted once idle")

	if got := started.all()[1]; got != "s-1" {
		t.Fatalf("resumed session = %q, want s-1", got)
	}

	if s.Pending("dev1@dev") {
		t.Fatal("the change is still pending after being applied")
	}
}

func TestApplyOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     Status
		opts       ApplyOptions
		wantResume string
	}{
		{name: "fresh: a new session", status: StatusIdle, opts: ApplyOptions{Fresh: true}, wantResume: ""},
		{name: "now: even mid-turn", status: StatusWorking, opts: ApplyOptions{Now: true}, wantResume: "s-1"},
		{name: "now and fresh", status: StatusAwaitingApproval, opts: ApplyOptions{Now: true, Fresh: true}, wantResume: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _, started := newApplySupervisor(t)
			s.Statuses.Set("dev1@dev", tt.status)

			if err := s.Apply(context.Background(), "dev1@dev", tt.opts); err != nil {
				t.Fatal(err)
			}

			got := started.all()
			if len(got) != 2 || got[1] != tt.wantResume {
				t.Fatalf("launches = %q, want a second launch resuming %q", got, tt.wantResume)
			}
		})
	}
}

func TestApplyStopsAnAgentOnLeaveOrGone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		org  *org.Org
	}{
		{name: "on leave", org: applyOrg(true)},
		{name: "dismissed", org: &org.Org{Teams: []org.Team{{ID: "dev"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, source, started := newApplySupervisor(t)
			source.set(tt.org)

			if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
				t.Fatal(err)
			}

			if _, running := s.Output("dev1@dev"); running {
				t.Fatal("the agent is still running")
			}

			if got := s.Statuses.Get("dev1@dev"); got != StatusStopped {
				t.Fatalf("status = %s, want stopped", got)
			}

			if len(started.all()) != 1 {
				t.Fatal("the agent was started again")
			}
		})
	}
}

func TestAgentOnLeaveIsNeverStarted(t *testing.T) {
	t.Parallel()

	s, source, _ := newApplySupervisor(t)
	source.set(applyOrg(true))

	if err := s.Stop("dev1@dev"); err != nil {
		t.Fatal(err)
	}

	if err := s.Start(context.Background(), "dev1@dev"); !errors.Is(err, ErrOnLeave) {
		t.Fatalf("Start of an agent on leave: err = %v, want ErrOnLeave", err)
	}

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll should skip agents on leave: %v", err)
	}

	if err := s.RestartAll(context.Background()); err != nil {
		t.Fatalf("RestartAll should skip agents on leave: %v", err)
	}

	if _, running := s.Output("dev1@dev"); running {
		t.Fatal("an agent on leave was started")
	}
}

func TestReturnFromLeaveResumesTheSession(t *testing.T) {
	t.Parallel()

	s, source, started := newApplySupervisor(t)

	source.set(applyOrg(true))

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	source.set(applyOrg(false))

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	if got := started.all(); len(got) != 2 || got[1] != "s-1" {
		t.Fatalf("launches = %q, want the return to resume s-1", got)
	}
}

func TestPendingChangeAppliesWhenTheProcessExits(t *testing.T) {
	t.Parallel()

	s, _, started := newApplySupervisor(t)
	s.Statuses.Set("dev1@dev", StatusWorking)

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	// The agent dies mid-turn (crash, /exit): it will never be idle.
	p, _ := s.process("dev1@dev")
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	eventually(t, func() bool { return len(started.all()) == 2 }, "agent restarted after exiting")
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopAllDropsPendingChanges(t *testing.T) {
	t.Parallel()

	s, _, started := newApplySupervisor(t)
	s.Statuses.Set("dev1@dev", StatusWorking)

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	s.StopAll()

	eventually(t, func() bool {
		_, running := s.Output("dev1@dev")

		return !running
	}, "dev1 stopped")

	if s.Pending("dev1@dev") || len(started.all()) != 1 {
		t.Fatal("a pending change must not restart an agent while the daemon shuts down")
	}

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); !errors.Is(err, ErrStopping) {
		t.Fatalf("Apply after StopAll: err = %v, want ErrStopping", err)
	}

	if err := s.Start(context.Background(), "dev1@dev"); !errors.Is(err, ErrStopping) {
		t.Fatalf("Start after StopAll: err = %v, want ErrStopping", err)
	}
}

func TestRestartAllTakesOverPendingChanges(t *testing.T) {
	t.Parallel()

	s, source, started := newApplySupervisor(t)

	var retired []string

	s.OnRetired = func(address string) { retired = append(retired, address) }

	s.Statuses.Set("dev1@dev", StatusWorking)
	source.set(applyOrg(true)) // put on leave while working: the change waits

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	// A feature starts meanwhile.
	if err := s.RestartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	if s.Pending("dev1@dev") || len(retired) != 1 || len(started.all()) != 1 {
		t.Fatalf("pending=%v retired=%v launches=%v: RestartAll should take the leave over and hand the work over",
			s.Pending("dev1@dev"), retired, started.all())
	}
}

func TestSessionIsOnlyResumedInItsDirectory(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		dir     = "/project"
		resumes []string
	)

	s := &Supervisor{
		Org:      &mutableOrg{o: applyOrg(false)},
		Statuses: NewStatuses(),
		Logger:   slog.New(slog.DiscardHandler),
		NewCommand: func(ctx context.Context, _ *org.Org, _ org.Member, resume string) (*exec.Cmd, error) {
			mu.Lock()
			defer mu.Unlock()

			resumes = append(resumes, resume)
			cmd := exec.CommandContext(ctx, "cat")
			cmd.Dir = dir

			return cmd, nil
		},
		HasMail: func(context.Context, string) (bool, error) { return false, nil },
	}
	t.Cleanup(s.StopAll)

	mu.Lock()
	dir = t.TempDir()
	mu.Unlock()

	if err := s.Start(context.Background(), "dev1@dev"); err != nil {
		t.Fatal(err)
	}

	s.Statuses.ObserveHook("dev1@dev", hook.Event{Name: hook.EventStop, SessionID: "s-1"}, 0)

	// The agent now works elsewhere (a feature's worktree).
	mu.Lock()
	dir = t.TempDir()
	mu.Unlock()

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if last := resumes[len(resumes)-1]; last != "" {
		t.Fatalf("launches = %q: a session started in another directory must not be resumed", resumes)
	}
}

func TestNoWakeUpBetweenIdleAndRestart(t *testing.T) {
	t.Parallel()

	s, _, _ := newApplySupervisor(t)
	s.Statuses.Set("dev1@dev", StatusWorking)

	if err := s.Apply(context.Background(), "dev1@dev", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	s.Statuses.OnIdle = nil // observe the status right after applyIfPending
	s.Statuses.Set("dev1@dev", StatusIdle)

	if !s.applyIfPending("dev1@dev") {
		t.Fatal("the pending change should be applied")
	}

	if got := s.Statuses.Get("dev1@dev"); got == StatusIdle {
		t.Fatal("the agent must not look idle (and be woken up for mail) while it is about to restart")
	}
}
