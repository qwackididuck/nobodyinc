package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// The director creates a team with team_create and recruits into it: the
// agents already there get a notice from nobody telling them who arrived and
// what to ask them about; the newcomer is not told about itself.
func TestDirectorCreatesATeam(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, withDirector(t, staffProject(t)))
	eventually(t, "god started", func() bool { return d.Statuses.Get("god") == runtime.StatusIdle })

	srv := httptest.NewServer(d.Handler)
	t.Cleanup(srv.Close)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "god-test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp/god"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	useTool(t, session, "team_create", map[string]any{"id": "qa", "goal": "Test it", "prompt": "Prove the tests can fail."})
	useTool(t, session, "agent_recruit", map[string]any{"team": "qa", "name": "tester", "template": "reviewer"})
	eventually(t, "tester@qa started", func() bool { return d.Statuses.Get("tester@qa") == runtime.StatusIdle })

	eventually(t, "dev1@dev told of the new team and its member", func() bool {
		told := strings.Join(orgNotices(t, d, "dev1@dev"), "\n")

		return strings.Contains(told, "New team qa: Test it.") && strings.Contains(told, "tester@qa (Code reviewer) joined team qa.")
	})

	for _, body := range orgNotices(t, d, "tester@qa") {
		if strings.Contains(body, "tester@qa") {
			t.Fatalf("tester@qa was told about itself: %s", body)
		}
	}
}

// A team created through the API has its TEAM.md, and its agent's system
// prompt holds the whole cascade: the project prompt, the team's goal and
// prompt, then its own role.
func TestTeamPromptsReachTheAgent(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	dir := live.Current().ProjectDir

	call(t, d, http.MethodPut, "/api/project", `{"prompt":"A shop in Go."}`, http.StatusOK)
	call(t, d, http.MethodPost, "/api/teams", `{"id":"qa","goal":"Test it","prompt":"Prove the tests can fail."}`, http.StatusCreated)
	call(t, d, http.MethodPost, "/api/teams", `{"id":"qa"}`, http.StatusConflict)

	if data, err := os.ReadFile(filepath.Join(dir, org.ConfigDirName, "teams", "qa", "TEAM.md")); err != nil ||
		strings.TrimSpace(string(data)) != "Prove the tests can fail." {
		t.Fatalf("TEAM.md = %q, %v", data, err)
	}

	if teams := string(call(t, d, http.MethodGet, "/api/teams", "", http.StatusOK)); !strings.Contains(teams, `"prompt":"Prove the tests can fail."`) {
		t.Fatalf("GET /api/teams = %s", teams)
	}

	call(t, d, http.MethodPost, "/api/teams/qa/agents", `{"name":"tester","template":"reviewer"}`, http.StatusCreated)
	eventually(t, "tester@qa started", func() bool { return d.Statuses.Get("tester@qa") == runtime.StatusIdle })

	_, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: "tester@qa", Act: messaging.ActQuery, Priority: messaging.Normal,
		Subject: "Prompt", Body: "SYSTEM PROMPT?",
	})
	if err != nil {
		t.Fatal(err)
	}

	var prompt string

	eventually(t, "tester@qa answered with its system prompt", func() bool {
		if m := messageFrom(t, d, "tester@qa", org.HumanAddress, messaging.ActInform); m != nil {
			prompt = m.Body
		}

		return prompt != ""
	})

	rest := prompt
	for _, want := range []string{"A shop in Go.", "Team goal: Test it", "Prove the tests can fail.", "## Your role: Code reviewer"} {
		i := strings.Index(rest, want)
		if i < 0 {
			t.Fatalf("system prompt misses %q, or has it out of order:\n%s", want, prompt)
		}

		rest = rest[i+len(want):]
	}
}

// orgNotices returns the bodies of the org-change notices sent to an agent.
func orgNotices(t *testing.T, d *daemon.Daemon, to string) []string {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}

	var bodies []string

	for _, m := range messages {
		if m.From == org.SystemAddress && m.To == to && m.Subject == "The organization changed" {
			bodies = append(bodies, m.Body)
		}
	}

	return bodies
}
