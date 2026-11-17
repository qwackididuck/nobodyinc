// Package e2e runs whole multi-agent scenarios through the real daemon, the
// real nobody-hook and real pseudo-terminals, with test/fakeclaude playing the
// agents. No model is involved: the scenarios are free and deterministic.
package e2e

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

var hookBinary, fakeClaude string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nobody-e2e-*")
	if err != nil {
		panic(err)
	}

	hookBinary = build(dir, "nobody-hook", "github.com/qwackididuck/nobodyinc/cmd/nobody-hook")
	fakeClaude = build(dir, "fakeclaude", "github.com/qwackididuck/nobodyinc/test/fakeclaude")

	code := m.Run()

	os.RemoveAll(dir)
	os.Exit(code)
}

func build(dir, name, pkg string) string {
	out := filepath.Join(dir, name)
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, pkg)

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("build " + name + ": " + err.Error())
	}

	return out
}

// testOrg: specs (archi) and dev (dev1), with no flow configured: any agent
// may start a conversation with any team (ADR-0009).
func testOrg(t *testing.T) *org.Org {
	t.Helper()

	return &org.Org{
		ProjectDir: t.TempDir(),
		Teams: []org.Team{
			{ID: "specs", Agents: []org.Agent{{Name: "archi", Role: "Architect"}}},
			{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}},
		},
	}
}

// startDaemon runs a full daemon on a free local port with every agent
// started, and waits until they are all idle. o is a fixed *org.Org, or an
// *org.Live for scenarios that change the organization.
func startDaemon(t *testing.T, o org.Source) *daemon.Daemon {
	t.Helper()

	return startDaemonWithDB(t, o, ":memory:")
}

// startDaemonWithDB is startDaemon on a given database file, e.g. one left by
// a previous run.
func startDaemonWithDB(t *testing.T, o org.Source, dbPath string) *daemon.Daemon {
	t.Helper()

	var lc net.ListenConfig

	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	d, err := daemon.New(ctx, daemon.Options{
		Logger:       slog.New(slog.DiscardHandler),
		Org:          o,
		CatalogDir:   t.TempDir(),
		WorktreesDir: t.TempDir(),
		DBPath:       dbPath,
		ClaudePath:   fakeClaude,
		HookPath:     hookBinary,
		DaemonURL:    "http://" + listener.Addr().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { d.Close() })

	srv := &http.Server{Handler: d.Handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	}()

	t.Cleanup(func() { srv.Close() })

	if err := d.Supervisor.StartAll(ctx); err != nil {
		t.Fatal(err)
	}

	for _, m := range o.Current().Members() {
		eventually(t, m.Address+" idle after onboarding", func() bool {
			return d.Statuses.Get(m.Address) == runtime.StatusIdle
		})
	}

	return d
}

func sendAsHuman(t *testing.T, d *daemon.Daemon, to, subject, body string) {
	t.Helper()

	_, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: to, Act: messaging.ActRequest, Priority: messaging.Normal,
		Subject: subject, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The human asks dev1 to consult the specs team: dev1 is woken up, asks archi
// (no flow needs to be declared), archi is woken up and answers, and dev1
// reports back to the human.
func TestDevConsultsSpecsAndReportsToHuman(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	sendAsHuman(t, d, "dev1@dev", "Pick a database", "ASK archi@specs: Which database for orders?")

	want := []string{
		"human -> dev1@dev [request]",
		"dev1@dev -> archi@specs [query]",
		"archi@specs -> dev1@dev [inform]",
		"dev1@dev -> human [done]",
	}

	eventually(t, "the full exchange", func() bool { return len(exchange(t, d)) >= len(want) })

	got := exchange(t, d)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exchange:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	for _, m := range []string{"archi@specs", "dev1@dev"} {
		eventually(t, m+" idle again", func() bool { return d.Statuses.Get(m) == runtime.StatusIdle })
	}
}

// Two teams without any configuration talk both ways: archi starts a
// conversation with dev1, as dev1 can with archi.
func TestTeamsWithoutConfigurationTalk(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	sendAsHuman(t, d, "archi@specs", "Check with dev", "ASK dev1@dev: Did you start?")

	want := []string{
		"human -> archi@specs [request]",
		"archi@specs -> dev1@dev [query]",
		"dev1@dev -> archi@specs [inform]",
		"archi@specs -> human [done]",
	}

	eventually(t, "the full exchange", func() bool { return len(exchange(t, d)) >= len(want) })

	if got := exchange(t, d); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exchange:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A block is one-way: with specs blocking dev, archi may not start a
// conversation with dev1. The refusal reaches archi as a tool error telling
// it to ask the human, which it does.
func TestBlockRefusalIsEscalated(t *testing.T) {
	t.Parallel()

	o := testOrg(t)
	o.Teams[0].Block = []string{"dev"}

	d := startDaemon(t, o)
	sendAsHuman(t, d, "archi@specs", "Tell dev", "ASK dev1@dev: Did you start?")

	eventually(t, "escalation to the human", func() bool { return len(exchange(t, d)) >= 2 })

	got := exchange(t, d)
	if got[1] != "archi@specs -> human [inform]" {
		t.Fatalf("exchange = %v, want archi escalating to the human", got)
	}

	messages, err := d.Store.RecentMessages(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(messages[1].Body, "may not start conversations with this team") {
		t.Fatalf("escalation should explain the refusal, got %q", messages[1].Body)
	}
}

// An urgent question to a busy agent is answered at once by a fork of its
// session; the agent gets a notice once its task is over. A question the fork
// cannot answer goes back to the agent's tray and is answered by the agent.
func TestUrgentQuestionToBusyAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		question     string
		wantFromFork bool
	}{
		{"answered by a fork", "Which database did we pick?", true},
		{"escalated: answered by the agent itself", "UNKNOWN: what will the client want next year?", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := startDaemon(t, testOrg(t))
			sendAsHuman(t, d, "archi@specs", "Design the API", "WORK 3")
			eventually(t, "archi busy", func() bool { return d.Statuses.Busy("archi@specs") })

			q, err := d.Messages.Send(context.Background(), messaging.Draft{
				From: "dev1@dev", To: "archi@specs", Act: messaging.ActQuery, Priority: messaging.Urgent,
				Subject: "DB?", Body: tt.question,
			})
			if err != nil {
				t.Fatal(err)
			}

			answer := waitForAnswer(t, d, q.ID)

			fromFork := strings.HasPrefix(answer.Body, "fork answer")
			if fromFork != tt.wantFromFork {
				t.Fatalf("answered by the fork = %v, want %v (body %q)", fromFork, tt.wantFromFork, answer.Body)
			}

			if tt.wantFromFork {
				if !d.Statuses.Busy("archi@specs") {
					t.Fatal("the fork should answer while archi is still busy")
				}

				eventually(t, "archi gets the notice", func() bool { return noticeDelivered(t, d, "archi@specs") })
			}
		})
	}
}

func waitForAnswer(t *testing.T, d *daemon.Daemon, questionID string) messaging.Message {
	t.Helper()

	var answer messaging.Message

	eventually(t, "an answer to "+questionID, func() bool {
		messages, err := d.Store.RecentMessages(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}

		for _, m := range messages {
			if m.InReplyTo == questionID {
				answer = m

				return true
			}
		}

		return false
	})

	return answer
}

func noticeDelivered(t *testing.T, d *daemon.Daemon, to string) bool {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range messages {
		if m.From == org.SystemAddress && m.To == to && m.DeliveredAt != nil {
			return true
		}
	}

	return false
}

// A ticket for the dev team reaches an idle dev through a notice; the dev
// claims it, attaches a deliverable, closes it, and the creator is told.
func TestTicketQueueToDone(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	ctx := context.Background()

	tk, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Build the order API", Team: "dev"})
	if err != nil {
		t.Fatal(err)
	}

	details := waitForDone(t, d, tk.ID)
	if details.Ticket.Assignee != "dev1@dev" || len(details.Deliverables) != 1 {
		t.Fatalf("finished ticket = %+v, deliverables %+v", details.Ticket, details.Deliverables)
	}

	eventually(t, "the human told the ticket is done", func() bool {
		return hasMessage(t, d, org.SystemAddress, org.HumanAddress, tk.ID)
	})
}

// A ticket blocked by another one is only announced once that one is done.
func TestBlockedTicketStartsWhenDependencyIsDone(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	ctx := context.Background()

	specs, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Write the API spec", Team: "specs"})
	if err != nil {
		t.Fatal(err)
	}

	impl, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Implement the API", Team: "dev", BlockedBy: []string{specs.ID}})
	if err != nil {
		t.Fatal(err)
	}

	specsDone := waitForDone(t, d, specs.ID)

	implDone := waitForDone(t, d, impl.ID)
	if specsDone.Ticket.Assignee != "archi@specs" || implDone.Ticket.Assignee != "dev1@dev" {
		t.Fatalf("assignees = %s, %s", specsDone.Ticket.Assignee, implDone.Ticket.Assignee)
	}

	if !implDone.Ticket.UpdatedAt.After(specsDone.Ticket.UpdatedAt) {
		t.Fatal("the dev ticket must be finished after the specs ticket it depends on")
	}
}

// A message to "@specs" reaches an available member of the specs team.
func TestTeamAddress(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	sendAsHuman(t, d, "dev1@dev", "Ask the team", "ASK @specs: Which database for orders?")

	eventually(t, "the exchange through @specs", func() bool { return len(exchange(t, d)) >= 4 })

	if got := exchange(t, d)[1]; got != "dev1@dev -> archi@specs [query]" {
		t.Fatalf("message to @specs went to %q", got)
	}
}

// waitForDone waits until a ticket is done.
func waitForDone(t *testing.T, d *daemon.Daemon, id string) work.Details {
	t.Helper()

	var details work.Details

	eventually(t, id+" done", func() bool {
		var err error

		details, err = d.Tickets.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}

		return details.Ticket.Status == work.StatusDone
	})

	return details
}

func hasMessage(t *testing.T, d *daemon.Daemon, from, to, ticket string) bool {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range messages {
		if m.From == from && m.To == to && m.Ticket == ticket {
			return true
		}
	}

	return false
}

// exchange summarizes the messages so far as "from -> to [act]".
func exchange(t *testing.T, d *daemon.Daemon) []string {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.From + " -> " + m.To + " [" + string(m.Act) + "]"
	}

	return lines
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// With a feature, work moves between teams through git: specs commits in its
// team's worktree and closes its ticket, which merges it into the feature
// branch; dev claims the next ticket and finds the specs in its own worktree.
func TestFeatureRelayThroughGit(t *testing.T) {
	t.Parallel()

	o := testOrg(t)
	initGitProject(t, o.ProjectDir)

	d := startDaemon(t, o)
	ctx := context.Background()

	before := outputs(d, o)

	f, err := d.Tickets.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	waitRestarted(t, d, o, before)

	specs, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Write the spec", Team: "specs"})
	if err != nil {
		t.Fatal(err)
	}

	impl, err := d.Tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Implement", Team: "dev", BlockedBy: []string{specs.ID}})
	if err != nil {
		t.Fatal(err)
	}

	waitForDone(t, d, specs.ID)
	waitForDone(t, d, impl.ID)

	files := git(t, o.ProjectDir, "ls-tree", "--name-only", f.Branch)
	for _, want := range []string{specs.ID + ".md", impl.ID + ".md"} {
		if !strings.Contains(files, want) {
			t.Fatalf("feature branch %s misses %s:\n%s", f.Branch, want, files)
		}
	}

	devDir := d.WorkDir("dev1@dev")
	if _, err := os.Stat(filepath.Join(devDir, specs.ID+".md")); err != nil {
		t.Fatal("dev should have received the specs in its worktree when claiming")
	}

	if authors := git(t, o.ProjectDir, "log", "--format=%an", f.Branch); !strings.Contains(authors, "archi@specs (Nobody Inc)") {
		t.Fatalf("commits should carry the agent's identity, authors:\n%s", authors)
	}

	final, err := d.Tickets.Get(ctx, specs.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !hasCommitDeliverable(final) {
		t.Fatalf("the delivered commit should be a deliverable of %s: %+v", specs.ID, final.Deliverables)
	}

	if branch := git(t, o.ProjectDir, "branch", "--show-current"); branch != "main" {
		t.Fatalf("the user's checkout must stay on main, got %q", branch)
	}
}

func initGitProject(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "--initial-branch=main")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("project\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "chore: init")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

// outputs records the current terminal of every agent, to detect restarts.
func outputs(d *daemon.Daemon, o *org.Org) map[string]*runtime.Output {
	current := map[string]*runtime.Output{}

	for _, m := range o.Members() {
		current[m.Address], _ = d.Supervisor.Output(m.Address)
	}

	return current
}

// waitRestarted waits until every agent runs a new process and is idle.
func waitRestarted(t *testing.T, d *daemon.Daemon, o *org.Org, before map[string]*runtime.Output) {
	t.Helper()

	for _, m := range o.Members() {
		eventually(t, m.Address+" restarted and idle", func() bool {
			now, ok := d.Supervisor.Output(m.Address)

			return ok && now != before[m.Address] && d.Statuses.Get(m.Address) == runtime.StatusIdle
		})
	}
}

func hasCommitDeliverable(d work.Details) bool {
	for _, x := range d.Deliverables {
		if x.Kind == "commit" && x.Reference != "" {
			return true
		}
	}

	return false
}
