package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// swappableOrg is an organization a test can change (an agent dismissed).
type swappableOrg struct {
	mu sync.Mutex
	o  *org.Org
}

func (s *swappableOrg) Current() *org.Org {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.o
}

func (s *swappableOrg) set(o *org.Org) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.o = o
}

// scheduleTest is the scheduled tasks wired as in the daemon, without
// agents: the test sets their statuses and plays their part.
type scheduleTest struct {
	t        *testing.T
	org      *swappableOrg
	store    *store.Store
	statuses *runtime.Statuses
	tickets  *work.Service
	messages *messaging.Service
	clock    *schedule.ManualClock
	office   *scheduleOffice
}

var scheduleT0 = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)

// newScheduleTest: team ops (watcher, backup; allowlist profile), the
// director god, a project directory of its own.
func newScheduleTest(t *testing.T) *scheduleTest {
	t.Helper()

	allowlist := catalog.Definition{Permissions: catalog.Permissions{Profile: catalog.ProfileAllowlist}}
	src := &swappableOrg{o: &org.Org{
		ProjectDir: t.TempDir(),
		Teams: []org.Team{{ID: "ops", Agents: []org.Agent{
			{Name: "watcher", Definition: allowlist}, {Name: "backup", Definition: allowlist},
		}}},
		God: &org.Agent{Name: "god"},
	}}

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	return newScheduleTestOn(t, src, st, schedule.NewManualClock(scheduleT0))
}

// newScheduleTestOn wires the scheduled tasks on an existing store, as a
// daemon restart does.
func newScheduleTestOn(t *testing.T, src *swappableOrg, st *store.Store, clock *schedule.ManualClock) *scheduleTest {
	t.Helper()

	statuses := runtime.NewStatuses()
	messages := &messaging.Service{Store: st, Policy: messaging.Policy{Org: src}}
	tickets := &work.Service{Store: st, Org: src}

	office, err := newScheduleOffice(context.Background(), Options{
		Org: src, Logger: slog.New(slog.DiscardHandler), Clock: clock,
	}, scheduleDeps{statuses: statuses, tickets: tickets, messages: messages, store: st})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	tickets.OnEvent = func(e work.Event) { office.work.onTicketEvent(ctx, e) }
	statuses.OnIdle = func(address string) { office.work.onIdle(ctx, address) }

	return &scheduleTest{
		t: t, org: src, store: st, statuses: statuses,
		tickets: tickets, messages: messages, clock: clock, office: office,
	}
}

func (s *scheduleTest) create(id, target string, mode schedule.Mode) {
	s.t.Helper()

	if _, err := s.office.Create(s.t.Context(), schedule.Schedule{
		ID: id, Title: "Task " + id, Every: "10m", Target: target, Mode: mode, Instructions: "Look at the dashboard.",
	}); err != nil {
		s.t.Fatal(err)
	}
}

// fire makes a schedule fall due now, as the scheduler would.
func (s *scheduleTest) fire(id string) {
	s.t.Helper()

	sc, ok := s.office.book.Get(id)
	if !ok {
		s.t.Fatalf("no schedule %s", id)
	}

	s.office.work.due(s.t.Context(), sc, s.clock.Now())
}

// runs returns a schedule's occurrences, oldest first, as "status agent".
func (s *scheduleTest) runs(id string) []string {
	s.t.Helper()

	runs, err := s.office.Runs(s.t.Context(), id, 0)
	if err != nil {
		s.t.Fatal(err)
	}

	got := make([]string, len(runs))
	for i, r := range runs {
		got[len(runs)-1-i] = strings.TrimSpace(string(r.Status) + " " + r.Agent)
	}

	return got
}

func (s *scheduleTest) expectRuns(id string, want ...string) {
	s.t.Helper()

	if got := s.runs(id); strings.Join(got, ", ") != strings.Join(want, ", ") {
		s.t.Fatalf("runs of %s = %v, want %v", id, got, want)
	}
}

func (s *scheduleTest) last(id string) schedule.Run {
	s.t.Helper()

	runs, err := s.office.Runs(s.t.Context(), id, 1)
	if err != nil || len(runs) != 1 {
		s.t.Fatalf("last run of %s: %v, %v", id, runs, err)
	}

	return runs[0]
}

// pending returns the subjects of an address's undelivered mail.
func (s *scheduleTest) pending(address string) []string {
	s.t.Helper()

	var subjects []string

	for _, p := range []messaging.Priority{messaging.Urgent, messaging.Normal} {
		msgs, err := s.store.PendingMessages(s.t.Context(), address, p)
		if err != nil {
			s.t.Fatal(err)
		}

		for _, m := range msgs {
			if m.From != org.SystemAddress && m.From != "god" && m.From != org.HumanAddress {
				s.t.Fatalf("mail to %s from %s", address, m.From)
			}

			subjects = append(subjects, m.Subject)
		}
	}

	return subjects
}

// read plays an agent reading its mail, then ending its turn.
func (s *scheduleTest) read(address string) {
	s.t.Helper()

	msgs, err := s.store.PendingMessages(s.t.Context(), address, messaging.Normal)
	if err != nil {
		s.t.Fatal(err)
	}

	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}

	if err := s.store.MarkDelivered(s.t.Context(), address, ids, s.clock.Now()); err != nil {
		s.t.Fatal(err)
	}

	s.statuses.Set(address, runtime.StatusIdle)
}

func TestScheduledMessage(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage)
	s.statuses.Set("watcher@ops", runtime.StatusIdle)

	s.fire("watch")
	s.expectRuns("watch", "running watcher@ops")

	if got := s.pending("watcher@ops"); len(got) != 1 || got[0] != "Scheduled task: Task watch" {
		t.Fatalf("mail to watcher = %v", got)
	}

	// An idle reported before the mail is read does not end it.
	s.statuses.Set("watcher@ops", runtime.StatusIdle)
	s.expectRuns("watch", "running watcher@ops")

	s.read("watcher@ops")
	s.expectRuns("watch", "done watcher@ops")

	if r := s.last("watch"); r.Message == "" || r.StartedAt == nil || r.EndedAt == nil || r.Trigger != schedule.TriggerSchedule {
		t.Fatalf("run = %+v", r)
	}
}

func TestScheduledTicketStaysOutOfTheFeature(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("report", "@ops", schedule.ModeTicket)

	if _, err := s.tickets.StartFeature(s.t.Context(), org.HumanAddress, work.FeatureDraft{Title: "Orders"}); err != nil {
		t.Fatal(err)
	}

	s.statuses.Set("watcher@ops", runtime.StatusWorking)
	s.statuses.Set("backup@ops", runtime.StatusIdle)

	run, err := s.office.RunNow(s.t.Context(), "report")
	if err != nil {
		t.Fatal(err)
	}

	if run.Status != schedule.RunRunning || run.Agent != "backup@ops" || run.Trigger != schedule.TriggerManual {
		t.Fatalf("Run now = %+v, want running for the free member", run)
	}

	tk, err := s.tickets.Store.GetTicket(s.t.Context(), run.Ticket)
	if err != nil {
		t.Fatal(err)
	}

	if tk.Feature != "" || tk.Assignee != "backup@ops" || tk.CreatedBy != org.SystemAddress || tk.Title != "Task report" {
		t.Fatalf("ticket = %+v, want assigned by nobody outside the feature", tk)
	}

	if _, err := s.tickets.SetStatus(s.t.Context(), "backup@ops", tk.ID, work.StatusDone); err != nil {
		t.Fatal(err)
	}

	s.expectRuns("report", "done backup@ops")
}

func TestOverlapIsSkippedAndRunNowRefused(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage)
	s.statuses.Set("watcher@ops", runtime.StatusIdle)

	s.fire("watch")
	s.fire("watch")
	s.expectRuns("watch", "running watcher@ops", "skipped")

	if _, err := s.office.RunNow(s.t.Context(), "watch"); !errors.Is(err, schedule.ErrBusy) {
		t.Fatalf("Run now during an occurrence: %v, want ErrBusy", err)
	}
}

func TestWaitsUntilATargetAgentIsFree(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage)

	// Working: it waits.
	s.statuses.Set("watcher@ops", runtime.StatusWorking)
	s.fire("watch")
	s.expectRuns("watch", "waiting")

	// Idle but with a ticket in progress: still waits.
	if _, err := s.tickets.Create(s.t.Context(), org.HumanAddress, work.Draft{Title: "Fix", Team: "ops", Assignee: "watcher@ops"}); err != nil {
		t.Fatal(err)
	}

	s.statuses.Set("watcher@ops", runtime.StatusIdle)
	s.expectRuns("watch", "waiting")

	// Ticket done: free at its next idle.
	if _, err := s.tickets.SetStatus(s.t.Context(), "watcher@ops", "T-1", work.StatusDone); err != nil {
		t.Fatal(err)
	}

	s.expectRuns("watch", "running watcher@ops")

	// The next occurrence waits behind it: skipped.
	s.fire("watch")
	s.expectRuns("watch", "running watcher@ops", "skipped")
}

func TestUnavailableTargetIsToldOncePerStreak(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage) // watcher is stopped

	s.fire("watch")
	s.fire("watch")
	s.expectRuns("watch", "unavailable", "unavailable")

	if got := s.pending(org.HumanAddress); len(got) != 1 || !strings.Contains(got[0], "no agent to run it") {
		t.Fatalf("human told %v, want one notice", got)
	}

	// It runs again, then stops again: told again.
	s.statuses.Set("watcher@ops", runtime.StatusIdle)
	s.fire("watch")
	s.read("watcher@ops")
	s.statuses.Set("watcher@ops", runtime.StatusExited)
	s.fire("watch")

	s.expectRuns("watch", "unavailable", "unavailable", "done watcher@ops", "unavailable")

	if got := s.pending(org.HumanAddress); len(got) != 2 {
		t.Fatalf("human told %v, want a second notice", got)
	}
}

func TestGoneTargetDisablesTheSchedule(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage)

	s.org.set(s.org.Current().WithoutAgent("watcher@ops"))
	s.fire("watch")

	s.expectRuns("watch", "unavailable")

	written, err := schedule.Load(s.org.Current().ProjectDir)
	if err != nil || len(written) != 1 || written[0].Enabled {
		t.Fatalf("schedules.yaml = %+v, %v; want the schedule disabled", written, err)
	}

	if _, ok := s.office.scheduler.Next("watch"); ok {
		t.Fatal("a disabled schedule is still armed")
	}

	if got := s.pending(org.HumanAddress); len(got) != 1 || !strings.Contains(got[0], "disabled") {
		t.Fatalf("human told %v", got)
	}

	if _, err := s.office.SetEnabled(s.t.Context(), "watch", true); !errors.Is(err, schedule.ErrInvalid) {
		t.Fatalf("resume with a gone target: %v, want ErrInvalid", err)
	}
}

func TestPauseEditAndDeleteCancelWhatWaits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(s *scheduleTest) error
		note   string
	}{
		{"pause", func(s *scheduleTest) error {
			_, err := s.office.SetEnabled(s.t.Context(), "watch", false)

			return err
		}, "paused"},
		{"edit", func(s *scheduleTest) error {
			_, err := s.office.Update(s.t.Context(), "watch", schedule.Schedule{
				Title: "Watch", Every: "15m", Target: "@ops", Mode: schedule.ModeMessage, Instructions: "Look.",
			})

			return err
		}, "edited"},
		{"delete", func(s *scheduleTest) error { return s.office.Delete(s.t.Context(), "watch") }, "deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newScheduleTest(t)
			s.create("watch", "watcher@ops", schedule.ModeMessage)
			s.statuses.Set("watcher@ops", runtime.StatusWorking)
			s.fire("watch")

			if err := tt.change(s); err != nil {
				t.Fatal(err)
			}

			if r := s.last("watch"); r.Status != schedule.RunCanceled || !strings.Contains(r.Note, tt.note) {
				t.Fatalf("waiting run after %s = %+v", tt.name, r)
			}
		})
	}
}

func TestEditKeepsIDAndEnabled(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("watch", "watcher@ops", schedule.ModeMessage)

	if _, err := s.office.SetEnabled(s.t.Context(), "watch", false); err != nil {
		t.Fatal(err)
	}

	v, err := s.office.Update(s.t.Context(), "watch", schedule.Schedule{
		Title: "Watch", Cron: "@hourly", Target: "@ops", Mode: schedule.ModeTicket, Instructions: "Look.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if v.ID != "watch" || v.Enabled || v.Cron != "@hourly" || v.NextRun != nil {
		t.Fatalf("edited = %+v", v)
	}

	if _, err := s.office.Update(s.t.Context(), "watch", schedule.Schedule{ID: "other"}); !errors.Is(err, schedule.ErrInvalid) {
		t.Fatalf("renaming through an edit: %v", err)
	}
}

func TestRestartSettlesLeftoverRuns(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	s.create("waits", "watcher@ops", schedule.ModeMessage)
	s.create("mail", "backup@ops", schedule.ModeMessage)
	s.create("open", "@ops", schedule.ModeTicket)
	s.create("closed", "@ops", schedule.ModeTicket)

	s.statuses.Set("watcher@ops", runtime.StatusWorking)
	s.fire("waits")

	s.statuses.Set("backup@ops", runtime.StatusIdle)
	s.fire("mail")

	// watcher becomes free and takes "waits": both are running in message mode.
	s.statuses.Set("watcher@ops", runtime.StatusIdle)
	s.expectRuns("waits", "running watcher@ops")

	// Two running ticket runs, one whose ticket was closed meanwhile.
	for _, id := range []string{"open", "closed"} {
		r, err := s.store.InsertRun(s.t.Context(), schedule.Run{
			ScheduleID: id, ScheduleTitle: id, Trigger: schedule.TriggerSchedule, Mode: schedule.ModeTicket,
			Status: schedule.RunRunning, DueAt: scheduleT0, Agent: "backup@ops",
		})
		if err != nil {
			t.Fatal(err)
		}

		tk, err := s.tickets.Create(s.t.Context(), org.SystemAddress, work.Draft{Title: id, Team: "ops", NoFeature: true})
		if err != nil {
			t.Fatal(err)
		}

		r.Ticket = tk.ID
		if err := s.store.UpdateRun(s.t.Context(), r); err != nil {
			t.Fatal(err)
		}

		if id == "closed" {
			if _, err := s.tickets.SetStatus(s.t.Context(), org.HumanAddress, tk.ID, work.StatusDone); err != nil {
				t.Fatal(err)
			}
		}
	}

	// The daemon restarts on the same database.
	restarted := newScheduleTestOn(t, s.org, s.store, s.clock)

	restarted.expectRuns("waits", "interrupted watcher@ops")
	restarted.expectRuns("mail", "interrupted backup@ops")
	restarted.expectRuns("open", "running backup@ops")
	restarted.expectRuns("closed", "done backup@ops")
}

func TestDirectorProposals(t *testing.T) {
	t.Parallel()

	s := newScheduleTest(t)
	proposal := schedule.Schedule{
		ID: "watch", Title: "Watch", Every: "10m", Target: "watcher@ops", Mode: schedule.ModeMessage, Instructions: "Look.",
	}

	if _, err := s.office.Propose(s.t.Context(), "watcher@ops", proposal); err == nil {
		t.Fatal("a team agent proposed a schedule")
	}

	p, err := s.office.Propose(s.t.Context(), "god", proposal)
	if err != nil {
		t.Fatal(err)
	}

	if p.ID != "SP-1" || p.Status != schedule.ProposalPending || p.ProposalMessage == "" {
		t.Fatalf("proposal = %+v", p)
	}

	if got := s.pending(org.HumanAddress); len(got) != 1 || !strings.HasPrefix(got[0], "Schedule proposal SP-1") {
		t.Fatalf("human got %v", got)
	}

	if len(s.office.book.List()) != 0 {
		t.Fatal("a proposal was written before the human accepted it")
	}

	edited := proposal
	edited.Every = "30m"

	accepted, err := s.office.Accept(s.t.Context(), "SP-1", &edited)
	if err != nil {
		t.Fatal(err)
	}

	if accepted.Status != schedule.ProposalAccepted || accepted.DecidedAt == nil {
		t.Fatalf("accepted = %+v", accepted)
	}

	if sc, ok := s.office.book.Get("watch"); !ok || sc.Every != "30m" || !sc.Enabled {
		t.Fatalf("written schedule = %+v, %v", sc, ok)
	}

	if _, err := s.office.Accept(s.t.Context(), "SP-1", nil); !errors.Is(err, schedule.ErrDecided) {
		t.Fatalf("accepting twice: %v", err)
	}

	if _, err := s.office.Propose(s.t.Context(), "god", proposal); !errors.Is(err, schedule.ErrExists) {
		t.Fatalf("proposing an existing id: %v", err)
	}

	proposal.ID = "report"

	if _, err := s.office.Propose(s.t.Context(), "god", proposal); err != nil {
		t.Fatal(err)
	}

	if _, err := s.office.Refuse(s.t.Context(), "SP-2", "too often"); err != nil {
		t.Fatal(err)
	}

	god := s.pending("god")
	if len(god) != 2 || !strings.Contains(god[0], "accepted") || !strings.Contains(god[1], "refused") {
		t.Fatalf("director got %v, want agree then refuse", god)
	}

	proposal.ID, proposal.Mode, proposal.Target = "mine", schedule.ModeTicket, "god"

	if _, err := s.office.Propose(s.t.Context(), "god", proposal); !errors.Is(err, schedule.ErrInvalid) {
		t.Fatalf("director as a ticket target: %v", err)
	}
}
