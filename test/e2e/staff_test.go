package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// staffProject writes a project whose dev team has two members, and returns
// it as a live organization, as nobodyd runs it.
func staffProject(t *testing.T) *org.Live {
	t.Helper()

	o := &org.Org{
		ProjectDir: t.TempDir(),
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
			{Name: "dev1", Role: "Developer"},
			{Name: "dev2", Role: "Developer"},
		}}},
	}
	if err := org.WriteProject(o); err != nil {
		t.Fatal(err)
	}

	loaded, err := org.LoadProject(o.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}

	return org.NewLive(loaded)
}

// changeDev1 rewrites dev1's agent file (nil removes it: dismissal), reloads
// the organization and applies the change, as the API will.
func changeDev1(t *testing.T, d *daemon.Daemon, live *org.Live, a *org.Agent) {
	t.Helper()

	dir := live.Current().ProjectDir

	var err error
	if a != nil {
		err = org.WriteAgent(dir, "dev", *a)
	} else {
		err = org.RemoveAgent(dir, "dev", "dev1")
	}

	if err != nil {
		t.Fatal(err)
	}

	if _, err := live.Reload(); err != nil {
		t.Fatal(err)
	}

	if err := d.Supervisor.Apply(context.WithoutCancel(t.Context()), "dev1@dev", runtime.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
}

// An agent goes on leave with a ticket in progress: once it stops, a teammate
// takes the ticket over; mail sent to it waits, and the sender is told; team
// addressing skips it. Back from leave, it resumes and reads its mail.
// Dismissed, its undelivered mail is returned to the senders.
func TestLeaveReturnAndDismissal(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	ctx := context.Background()

	// dev1 holds a ticket in progress (inserted directly: no notice, so the
	// fake agent does not finish it on its own).
	tk, err := d.Store.InsertTicket(ctx, work.Ticket{
		Title: "Half-done work", Team: "dev", Assignee: "dev1@dev", Status: work.StatusInProgress, CreatedBy: org.HumanAddress,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Leave.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer", OnLeave: true})

	eventually(t, "dev1 stopped", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusStopped })

	details := waitForDone(t, d, tk.ID)
	if details.Ticket.Assignee != "dev2@dev" {
		t.Fatalf("ticket finished by %q, want the teammate dev2@dev", details.Ticket.Assignee)
	}

	if len(details.Comments) == 0 || details.Comments[0].Author != org.SystemAddress {
		t.Fatalf("the release should be explained in a comment from nobody: %+v", details.Comments)
	}

	sent := send(t, d, "dev1@dev", "While you are away")
	eventually(t, "the sender told dev1 is on leave", func() bool {
		return hasNotice(t, d, org.HumanAddress, "dev1@dev is on leave")
	})

	if pending := pendingFor(t, d, "dev1@dev"); len(pending) != 1 || pending[0].ID != sent.ID {
		t.Fatalf("mail to an agent on leave should wait in its tray, got %+v", pending)
	}

	if got := send(t, d, "@dev", "Anyone?"); got.To != "dev2@dev" {
		t.Fatalf("@dev picked %s, want dev2@dev (dev1 is on leave)", got.To)
	}

	// Return: resumed, the waiting mail is delivered with its first prompt.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer"})

	eventually(t, "dev1 back and idle", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusIdle })
	eventually(t, "the waiting mail delivered on return", func() bool { return len(pendingFor(t, d, "dev1@dev")) == 0 })

	// Dismissal: mail that can no longer be read goes back to its sender.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer", OnLeave: true})
	eventually(t, "dev1 on leave again", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusStopped })

	send(t, d, "dev1@dev", "Never read")
	changeDev1(t, d, live, nil)

	eventually(t, "the sender told the message was not delivered", func() bool {
		return hasNotice(t, d, org.HumanAddress, "Message not delivered: Never read")
	})

	if pending := pendingFor(t, d, "dev1@dev"); len(pending) != 0 {
		t.Fatalf("a dismissed agent's tray should be cleared, got %+v", pending)
	}
}

func send(t *testing.T, d *daemon.Daemon, to, subject string) messaging.Message {
	t.Helper()

	m, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: to, Act: messaging.ActInform, Priority: messaging.Normal, Subject: subject,
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func pendingFor(t *testing.T, d *daemon.Daemon, address string) []messaging.Message {
	t.Helper()

	var all []messaging.Message

	for _, p := range []messaging.Priority{messaging.Normal, messaging.Urgent} {
		pending, err := d.Store.PendingMessages(context.Background(), address, p)
		if err != nil {
			t.Fatal(err)
		}

		all = append(all, pending...)
	}

	return all
}

func hasNotice(t *testing.T, d *daemon.Daemon, to, subject string) bool {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range messages {
		if m.From == org.SystemAddress && m.To == to && m.Subject == subject {
			return true
		}
	}

	return false
}

// The whole lifecycle through the HTTP API, as the UI will drive it: recruit
// from a template, edit (the agent restarts), leave, return, dismiss.
func TestAgentLifecycleThroughTheAPI(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, staffProject(t))

	call(t, d, http.MethodPost, "/api/teams/dev/agents", `{"name":"rev","template":"reviewer"}`, http.StatusCreated)
	eventually(t, "rev@dev started", func() bool { return d.Statuses.Get("rev@dev") == runtime.StatusIdle })

	if a := agentByAddress(t, d, "rev@dev"); !strings.HasPrefix(a.From, "reviewer@") {
		t.Fatalf("recruit from = %q", a.From)
	}

	// Refusals.
	call(t, d, http.MethodPost, "/api/teams/dev/agents", `{"name":"rev","template":"reviewer"}`, http.StatusConflict)
	call(t, d, http.MethodPost, "/api/teams/ghosts/agents", `{"name":"x","template":"reviewer"}`, http.StatusNotFound)
	call(t, d, http.MethodPost, "/api/teams/dev/agents", `{"name":"x","template":"dev","definition":{}}`, http.StatusBadRequest)
	call(t, d, http.MethodPut, "/api/agents/rev@dev/definition", `{"permissions":{"profile":"yolo"}}`, http.StatusBadRequest)

	// Edit: the agent restarts with its new definition.
	before, _ := d.Supervisor.Output("rev@dev")
	call(t, d, http.MethodPut, "/api/agents/rev@dev/definition", `{"role":"Senior reviewer","prompt":"Review carefully."}`, http.StatusOK)
	eventually(t, "rev@dev restarted", func() bool {
		after, running := d.Supervisor.Output("rev@dev")

		return running && after != before && d.Statuses.Get("rev@dev") == runtime.StatusIdle
	})

	var def struct {
		Role   string `json:"role"`
		From   string `json:"from"`
		Prompt string `json:"prompt"`
	}
	decodeBody(t, call(t, d, http.MethodGet, "/api/agents/rev@dev/definition", "", http.StatusOK), &def)

	if def.Role != "Senior reviewer" || def.Prompt != "Review carefully." || !strings.HasPrefix(def.From, "reviewer@") {
		t.Fatalf("definition after edit = %+v", def)
	}

	// Leave, return, dismiss.
	call(t, d, http.MethodPost, "/api/agents/rev@dev/leave", "", http.StatusAccepted)
	eventually(t, "rev@dev on leave", func() bool { return d.Statuses.Get("rev@dev") == runtime.StatusStopped })

	if !agentByAddress(t, d, "rev@dev").OnLeave {
		t.Fatal("GET /api/agents does not show the leave")
	}

	call(t, d, http.MethodPost, "/api/agents/rev@dev/return", "", http.StatusAccepted)
	eventually(t, "rev@dev back", func() bool { return d.Statuses.Get("rev@dev") == runtime.StatusIdle })

	call(t, d, http.MethodDelete, "/api/agents/rev@dev", "", http.StatusAccepted)
	eventually(t, "rev@dev gone", func() bool {
		_, running := d.Supervisor.Output("rev@dev")

		return !running && !hasAgent(t, d, "rev@dev")
	})

	call(t, d, http.MethodDelete, "/api/agents/rev@dev", "", http.StatusNotFound)

	// User templates: saved, then deleted; built-in ones cannot be deleted.
	call(t, d, http.MethodPut, "/api/catalog/my-reviewer", `{"description":"Mine","version":1,"role":"Reviewer"}`, http.StatusOK)
	call(t, d, http.MethodDelete, "/api/catalog/my-reviewer", "", http.StatusNoContent)
	call(t, d, http.MethodGet, "/api/catalog/my-reviewer", "", http.StatusNotFound)
	call(t, d, http.MethodDelete, "/api/catalog/reviewer", "", http.StatusConflict)
}

type apiAgent struct {
	Address string `json:"address"`
	Role    string `json:"role"`
	From    string `json:"from"`
	OnLeave bool   `json:"on_leave"`
}

func call(t *testing.T, d *daemon.Daemon, method, path, body string, want int) []byte {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json") // as the control room sends it

	rec := httptest.NewRecorder()
	d.Handler.ServeHTTP(rec, req)

	if rec.Code != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
	}

	return rec.Body.Bytes()
}

func decodeBody(t *testing.T, data []byte, v any) {
	t.Helper()

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

func agents(t *testing.T, d *daemon.Daemon) []apiAgent {
	t.Helper()

	var list []apiAgent
	decodeBody(t, call(t, d, http.MethodGet, "/api/agents", "", http.StatusOK), &list)

	return list
}

func hasAgent(t *testing.T, d *daemon.Daemon, address string) bool {
	t.Helper()

	return slices.ContainsFunc(agents(t, d), func(a apiAgent) bool { return a.Address == address })
}

func agentByAddress(t *testing.T, d *daemon.Daemon, address string) apiAgent {
	t.Helper()

	for _, a := range agents(t, d) {
		if a.Address == address {
			return a
		}
	}

	t.Fatalf("%s not in GET /api/agents", address)

	return apiAgent{}
}

// A dismissal still waiting for the end of a turn is lost when the daemon
// stops. At the next start, what the departed agent left behind is handed
// over: its ticket goes back to the queue (and a teammate finishes it), its
// undelivered mail is returned to the sender.
func TestStartupHandsOverWhatDepartedAgentsLeft(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	dbPath := filepath.Join(t.TempDir(), "nobody.db")

	// What the previous run left: ghost@dev was dismissed (no file any more)
	// with a ticket in progress and a message it never read.
	previous, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}

	tk, err := previous.InsertTicket(context.Background(), work.Ticket{
		Title: "Left behind", Team: "dev", Assignee: "ghost@dev", Status: work.StatusInProgress, CreatedBy: org.HumanAddress,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = previous.InsertMessage(context.Background(), messaging.Message{
		ID: "m-ghost", Conversation: "m-ghost", From: org.HumanAddress, To: "ghost@dev",
		Act: messaging.ActInform, Priority: messaging.Normal, Subject: "Never read", SentAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	previous.Close()

	d := startDaemonWithDB(t, live, dbPath)

	if done := waitForDone(t, d, tk.ID); !strings.HasPrefix(done.Ticket.Assignee, "dev") || done.Ticket.Assignee == "ghost@dev" {
		t.Fatalf("ticket finished by %q, want a member of dev", done.Ticket.Assignee)
	}

	eventually(t, "the sender told the message was not delivered", func() bool {
		return hasNotice(t, d, org.HumanAddress, "Message not delivered: Never read")
	})
}

// The director is recruited through the API and, having no team, talks to
// any agent without a channel: asked by the human, it questions dev1@dev and
// gets the answer. There can be only one.
func TestDirector(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, staffProject(t))

	call(t, d, http.MethodPost, "/api/director", `{"name":"god","template":"god"}`, http.StatusCreated)
	eventually(t, "god started", func() bool { return d.Statuses.Get("god") == runtime.StatusIdle })
	call(t, d, http.MethodPost, "/api/director", `{"name":"boss","template":"god"}`, http.StatusConflict)

	sendAsHuman(t, d, "god", "Check with dev", "ASK dev1@dev: Is the build green?")

	eventually(t, "god asked dev1 and got the answer", func() bool {
		lines := strings.Join(exchange(t, d), "\n")

		return strings.Contains(lines, "god -> dev1@dev [query]") && strings.Contains(lines, "dev1@dev -> god")
	})

	if a := agentByAddress(t, d, "god"); a.Role != "Director" {
		t.Fatalf("GET /api/agents shows the director as %+v", a)
	}
}

// The director's office tools, called by a real MCP client as the director
// would: recruit an agent, create a ticket for a team (no channel needed),
// read the overview, dismiss the agent.
func TestDirectorToolsThroughMCP(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, staffProject(t))
	call(t, d, http.MethodPost, "/api/director", `{"name":"god","template":"god"}`, http.StatusCreated)
	eventually(t, "god started", func() bool { return d.Statuses.Get("god") == runtime.StatusIdle })

	srv := httptest.NewServer(d.Handler)
	t.Cleanup(srv.Close)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "god-test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp/god"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	useTool(t, session, "agent_recruit", map[string]any{"team": "dev", "name": "rev", "template": "reviewer"})
	eventually(t, "rev@dev started", func() bool { return d.Statuses.Get("rev@dev") == runtime.StatusIdle })

	var tk struct {
		ID string `json:"id"`
	}
	decodeBody(t, []byte(useTool(t, session, "ticket_create", map[string]any{"title": "Build the cart", "team": "dev"})), &tk)

	if done := waitForDone(t, d, tk.ID); done.Ticket.CreatedBy != "god" {
		t.Fatalf("ticket created by %q, want god", done.Ticket.CreatedBy)
	}

	var overview struct {
		Teams []struct {
			Team   string `json:"team"`
			Agents []struct {
				Address string `json:"address"`
			} `json:"agents"`
			Done int `json:"done"`
		} `json:"teams"`
	}
	decodeBody(t, []byte(useTool(t, session, "org_overview", nil)), &overview)

	if len(overview.Teams) != 1 || len(overview.Teams[0].Agents) != 3 || overview.Teams[0].Done != 1 {
		t.Fatalf("overview = %+v, want team dev with dev1, dev2, rev and one ticket done", overview)
	}

	useTool(t, session, "agent_dismiss", map[string]any{"address": "rev@dev"})
	eventually(t, "rev@dev dismissed", func() bool { return !hasAgent(t, d, "rev@dev") })
}

// useTool calls an MCP tool and returns its text result, failing on errors.
func useTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	var text strings.Builder

	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}

	if res.IsError {
		t.Fatalf("%s failed: %s", name, text.String())
	}

	return text.String()
}
