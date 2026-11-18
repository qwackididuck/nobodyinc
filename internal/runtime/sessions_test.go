package runtime

import (
	"context"
	"log/slog"
	"maps"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Where the agents of these tests work: existing directories, since their
// processes start there.
const (
	projectDir  = "/"
	worktreeDir = "/tmp"
)

// memorySessions is a SessionStore in memory.
type memorySessions struct {
	mu        sync.Mutex
	saved     map[string]SavedSession
	forgotten []string
}

func newMemorySessions(saved map[string]SavedSession) *memorySessions {
	return &memorySessions{saved: maps.Clone(saved)}
}

func (m *memorySessions) Sessions(context.Context) (map[string]SavedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Clone(m.saved), nil
}

func (m *memorySessions) SaveSession(_ context.Context, address, sessionID, workDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.saved == nil {
		m.saved = map[string]SavedSession{}
	}

	midTurn := m.saved[address].ID == sessionID && m.saved[address].MidTurn
	m.saved[address] = SavedSession{ID: sessionID, WorkDir: workDir, MidTurn: midTurn}

	return nil
}

func (m *memorySessions) SetMidTurn(_ context.Context, address string, midTurn bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.saved[address]; ok {
		s.MidTurn = midTurn
		m.saved[address] = s
	}

	return nil
}

func (m *memorySessions) ForgetSession(_ context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.saved, address)
	m.forgotten = append(m.forgotten, address)

	return nil
}

// dev1 returns the session remembered for dev1@dev, the agent of these
// tests.
func (m *memorySessions) dev1() (SavedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.saved["dev1@dev"]

	return s, ok
}

// reopenRecorder records how the supervisor launches dev1@dev. A resume of a
// session in lost runs `false`, which exits at once like claude resuming a
// session it does not have; anything else runs `cat`.
type reopenRecorder struct {
	mu      sync.Mutex
	resumes []Resume
	dir     string
	lost    string
}

func (r *reopenRecorder) command(ctx context.Context, _ *org.Org, _ org.Member, resume Resume) (*exec.Cmd, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resumes = append(r.resumes, resume)

	cmd := exec.CommandContext(ctx, "cat")
	if resume.Session != "" && resume.Session == r.lost {
		cmd = exec.CommandContext(ctx, "false")
	}

	cmd.Dir = r.dir

	return cmd, nil
}

func (r *reopenRecorder) all() []Resume {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.resumes)
}

func newReopenSupervisor(t *testing.T, sessions *memorySessions, r *reopenRecorder) *Supervisor {
	t.Helper()

	o := &org.Org{ProjectDir: projectDir, Teams: []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}}}}
	s := &Supervisor{
		Org:        o,
		Statuses:   NewStatuses(),
		Logger:     slog.New(slog.DiscardHandler),
		NewCommand: r.command,
		Sessions:   sessions,
		ReopenContext: func(*org.Org, org.Member) (string, error) {
			return "the current organization", nil
		},
		HasMail: func(context.Context, string) (bool, error) { return false, nil },
	}
	t.Cleanup(s.StopAll)

	return s
}

func TestStartAllResumesRememberedSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		saved      SavedSession
		wantResume Resume
		wantIdle   bool
	}{
		{
			name:       "an idle agent resumes silently and is idle at its SessionStart",
			saved:      SavedSession{ID: "s-1", WorkDir: projectDir},
			wantResume: Resume{Session: "s-1", Mode: ResumeSilently},
			wantIdle:   true,
		},
		{
			name:       "an interrupted agent resumes with a prompt to carry on",
			saved:      SavedSession{ID: "s-1", WorkDir: projectDir, MidTurn: true},
			wantResume: Resume{Session: "s-1", Mode: ResumeInterrupted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sessions := newMemorySessions(map[string]SavedSession{"dev1@dev": tt.saved})
			r := &reopenRecorder{dir: projectDir}
			s := newReopenSupervisor(t, sessions, r)

			if err := s.StartAll(context.Background()); err != nil {
				t.Fatal(err)
			}

			if got := r.all(); !slices.Equal(got, []Resume{tt.wantResume}) {
				t.Fatalf("launches = %+v, want %+v", got, tt.wantResume)
			}

			reopen, idle := s.SessionStarted("dev1@dev")
			if reopen != "the current organization" || idle != tt.wantIdle {
				t.Fatalf("SessionStarted = %q, %v; want the organization and idle=%v", reopen, idle, tt.wantIdle)
			}

			// A later SessionStart of the same process (/clear, compaction)
			// is not a reopening.
			if reopen, idle := s.SessionStarted("dev1@dev"); reopen != "" || idle {
				t.Fatalf("second SessionStarted = %q, %v; want nothing", reopen, idle)
			}
		})
	}
}

func TestStartAllStartsAfreshElsewhere(t *testing.T) {
	t.Parallel()

	sessions := newMemorySessions(map[string]SavedSession{"dev1@dev": {ID: "s-1", WorkDir: projectDir}})
	r := &reopenRecorder{dir: worktreeDir} // a feature moved the agent
	s := newReopenSupervisor(t, sessions, r)

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := r.all()
	if last := got[len(got)-1]; last != (Resume{}) {
		t.Fatalf("launches = %+v: a session started in another directory must not be resumed", got)
	}

	if _, ok := sessions.dev1(); ok {
		t.Fatal("the old session must be forgotten when the agent starts afresh")
	}

	if reopen, idle := s.SessionStarted("dev1@dev"); reopen != "" || idle {
		t.Fatalf("SessionStarted of a fresh session = %q, %v; want nothing", reopen, idle)
	}
}

func TestALostSessionStartsAfresh(t *testing.T) {
	t.Parallel()

	sessions := newMemorySessions(map[string]SavedSession{"dev1@dev": {ID: "s-gone", WorkDir: projectDir}})
	r := &reopenRecorder{dir: projectDir, lost: "s-gone"}
	s := newReopenSupervisor(t, sessions, r)

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := []Resume{{Session: "s-gone", Mode: ResumeSilently}, {}}

	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(r.all(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("launches = %+v, want %+v", r.all(), want)
		}

		time.Sleep(20 * time.Millisecond)
	}

	if _, ok := sessions.dev1(); ok {
		t.Fatal("the lost session must be forgotten")
	}
}

func TestAStoppedResumeIsNotALostSession(t *testing.T) {
	t.Parallel()

	sessions := newMemorySessions(map[string]SavedSession{"dev1@dev": {ID: "s-1", WorkDir: projectDir}})
	r := &reopenRecorder{dir: projectDir}
	s := newReopenSupervisor(t, sessions, r)

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := s.Stop("dev1@dev"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	if got := r.all(); len(got) != 1 {
		t.Fatalf("launches = %+v: an agent stopped on purpose must not be started again", got)
	}

	if _, ok := sessions.dev1(); !ok {
		t.Fatal("stopping an agent must keep its session")
	}
}

func TestSessionsAreRememberedFromHooks(t *testing.T) {
	t.Parallel()

	sessions := newMemorySessions(nil)
	r := &reopenRecorder{dir: projectDir}
	s := newReopenSupervisor(t, sessions, r)
	s.Statuses.OnSession = func(address, id string) { s.RememberSession(t.Context(), address, id) }
	s.Statuses.OnChange = func(address string, st Status) { s.ObserveStatus(t.Context(), address, st) }

	if err := s.Start(context.Background(), "dev1@dev"); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		status Status
		want   SavedSession
	}{
		{status: StatusWorking, want: SavedSession{ID: "s-7", WorkDir: projectDir, MidTurn: true}},
		{status: StatusIdle, want: SavedSession{ID: "s-7", WorkDir: projectDir}},
		{status: StatusAwaitingApproval, want: SavedSession{ID: "s-7", WorkDir: projectDir, MidTurn: true}},
		// Stopping with the daemon says nothing about the turn.
		{status: StatusExited, want: SavedSession{ID: "s-7", WorkDir: projectDir, MidTurn: true}},
	}

	s.RememberSession(t.Context(), "dev1@dev", "s-7")

	for _, step := range steps {
		s.Statuses.Set("dev1@dev", step.status)

		if got, _ := sessions.dev1(); got != step.want {
			t.Fatalf("after %s: session = %+v, want %+v", step.status, got, step.want)
		}
	}
}
