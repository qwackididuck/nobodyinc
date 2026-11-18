package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Scheduled tasks (ADR-0012) through the real daemon, hooks and PTYs. The
// daemon's clock is a schedule.ManualClock: a test moves it forward a minute
// to make an "every 1m" schedule fall due, so nothing waits for real time
// except the agents' own turns.

// startScheduledDaemon is startDaemon with a manual clock.
func startScheduledDaemon(t *testing.T, o org.Source) (*daemon.Daemon, *schedule.ManualClock) {
	t.Helper()

	clock := schedule.NewManualClock(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))
	d := startDaemonWithOptions(t, o, ":memory:", func(opts *daemon.Options) { opts.Clock = clock })

	return d, clock
}

// tick makes the schedules armed a minute ago fall due, once the scheduler
// waits for them.
func tick(clock *schedule.ManualClock) {
	clock.WaitForTimers(1)
	clock.Advance(time.Minute)
}

// createSchedule creates a schedule falling due every minute, as the
// control room does.
func createSchedule(t *testing.T, d *daemon.Daemon, id, target string, mode schedule.Mode) {
	t.Helper()

	body, err := json.Marshal(schedule.Schedule{
		ID: id, Title: "Task " + id, Every: "1m", Target: target, Mode: mode, Instructions: "Look at the dashboard.",
	})
	if err != nil {
		t.Fatal(err)
	}

	call(t, d, http.MethodPost, "/api/schedules", string(body), http.StatusCreated)
}

// runStatuses returns a schedule's occurrences, oldest first.
func runStatuses(t *testing.T, d *daemon.Daemon, id string) []schedule.RunStatus {
	t.Helper()

	runs, err := d.Store.ListRuns(context.Background(), store.RunFilter{Schedule: id})
	if err != nil {
		t.Fatal(err)
	}

	statuses := make([]schedule.RunStatus, len(runs))
	for i, r := range runs {
		statuses[len(runs)-1-i] = r.Status
	}

	return statuses
}

func waitForRuns(t *testing.T, d *daemon.Daemon, id string, want ...schedule.RunStatus) {
	t.Helper()

	join := func(s []schedule.RunStatus) string {
		parts := make([]string, len(s))
		for i, x := range s {
			parts[i] = string(x)
		}

		return strings.Join(parts, ",")
	}

	eventually(t, id+" runs "+join(want), func() bool { return join(runStatuses(t, d, id)) == join(want) })
}

func lastRun(t *testing.T, d *daemon.Daemon, id string) schedule.Run {
	t.Helper()

	runs, err := d.Store.ListRuns(context.Background(), store.RunFilter{Schedule: id, Limit: 1})
	if err != nil || len(runs) != 1 {
		t.Fatalf("last run of %s: %v, %v", id, runs, err)
	}

	return runs[0]
}

// In message mode, an occurrence is mail from nobody, over once the agent has
// read it and ended its turn. While the agent works on something else, the
// next occurrence waits for it, and the one after is skipped.
func TestScheduledMessageWaitsForTheAgent(t *testing.T) {
	t.Parallel()

	d, clock := startScheduledDaemon(t, testOrg(t))
	createSchedule(t, d, "watch", "archi@specs", schedule.ModeMessage)

	tick(clock)
	waitForRuns(t, d, "watch", schedule.RunDone)

	if r := lastRun(t, d, "watch"); r.Agent != "archi@specs" || r.Message == "" || r.EndedAt == nil {
		t.Fatalf("occurrence = %+v", r)
	}

	if m := messageFrom(t, d, org.SystemAddress, "archi@specs", messaging.ActInform); m == nil || m.DeliveredAt == nil ||
		!strings.Contains(m.Body, "Look at the dashboard.") {
		t.Fatalf("mail from nobody = %+v, want the instructions, delivered", m)
	}

	// archi works for a few seconds: the next occurrence waits, the one after
	// is skipped, and the waiting one runs once archi is idle again.
	sendAsHuman(t, d, "archi@specs", "Busy", "WORK 3")
	eventually(t, "archi working", func() bool { return d.Statuses.Busy("archi@specs") })

	tick(clock)
	waitForRuns(t, d, "watch", schedule.RunDone, schedule.RunWaiting)

	tick(clock)
	waitForRuns(t, d, "watch", schedule.RunDone, schedule.RunDone, schedule.RunSkipped)
}

// In ticket mode during an active feature, the occurrence is a ticket for a
// free member of the team, created by nobody outside the feature: what the
// agent commits for it is not delivered to the feature branch.
func TestScheduledTicketStaysOutOfTheFeature(t *testing.T) {
	t.Parallel()

	o := testOrg(t)
	initGitProject(t, o.ProjectDir)

	d, clock := startScheduledDaemon(t, o)
	before := outputs(d, o)

	f, err := d.Tickets.StartFeature(context.Background(), org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	waitRestarted(t, d, o, before)
	createSchedule(t, d, "report", "@dev", schedule.ModeTicket)

	tick(clock)
	waitForRuns(t, d, "report", schedule.RunDone)

	r := lastRun(t, d, "report")

	tk, err := d.Tickets.Get(context.Background(), r.Ticket)
	if err != nil {
		t.Fatal(err)
	}

	if tk.Ticket.Feature != "" || tk.Ticket.CreatedBy != org.SystemAddress || tk.Ticket.Assignee != "dev1@dev" ||
		tk.Ticket.Status != work.StatusDone {
		t.Fatalf("scheduled ticket = %+v, want done by dev1, created by nobody, outside the feature", tk.Ticket)
	}

	if files := git(t, o.ProjectDir, "ls-tree", "--name-only", f.Branch); strings.Contains(files, tk.Ticket.ID+".md") {
		t.Fatalf("the feature branch got the scheduled ticket's work:\n%s", files)
	}
}

// The director proposes a schedule, the human accepts it edited: it is
// written to schedules.yaml. Its target is dismissed: the next occurrence
// disables it and tells the human.
func TestProposedScheduleAndGoneTarget(t *testing.T) {
	t.Parallel()

	live := withDirector(t, staffProject(t))
	d, clock := startScheduledDaemon(t, live)

	srv := httptest.NewServer(d.Handler)
	t.Cleanup(srv.Close)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "god-test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp/god"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	if text := useTool(t, session, "schedule_propose", map[string]any{
		"id": "watch", "title": "Watch", "every": "10m", "target": "dev2@dev", "mode": "message", "instructions": "Look.",
	}); !strings.Contains(text, `"id":"SP-1"`) {
		t.Fatalf("schedule_propose = %s", text)
	}

	edited := `{"schedule":{"id":"watch","title":"Watch","every":"1m","target":"dev2@dev","mode":"message","instructions":"Look."}}`
	call(t, d, http.MethodPost, "/api/schedule-proposals/SP-1/accept", edited, http.StatusOK)

	written, err := schedule.Load(live.Current().ProjectDir)
	if err != nil || len(written) != 1 || written[0].Every != "1m" || !written[0].Enabled {
		t.Fatalf("schedules.yaml = %+v, %v", written, err)
	}

	eventually(t, "the director told", func() bool { return messageFrom(t, d, org.HumanAddress, "god", messaging.ActAgree) != nil })

	call(t, d, http.MethodDelete, "/api/agents/dev2@dev?now=true", "", http.StatusAccepted)
	eventually(t, "dev2 dismissed", func() bool {
		return !hasAgent(t, d, "dev2@dev") && d.Statuses.Get("dev2@dev") != runtime.StatusIdle
	})

	tick(clock)
	waitForRuns(t, d, "watch", schedule.RunUnavailable)

	eventually(t, "the human told", func() bool {
		m := messageFrom(t, d, org.SystemAddress, org.HumanAddress, messaging.ActInform)

		return m != nil && m.Subject == "Schedule watch disabled"
	})

	if written, err := schedule.Load(live.Current().ProjectDir); err != nil || written[0].Enabled {
		t.Fatalf("schedules.yaml = %+v, %v; want the schedule disabled", written, err)
	}
}
