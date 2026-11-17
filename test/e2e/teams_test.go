package e2e

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/work"
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

// A team's block list is changed through the API without restarting
// anyone: the members of the team that blocks and of the team blocked get a
// notice, and the block is enforced at once. Deleting the blocked team takes
// it out of the block list.
func TestBlockThroughTheAPI(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)

	call(t, d, http.MethodPost, "/api/teams", `{"id":"qa","goal":"Test it"}`, http.StatusCreated)
	call(t, d, http.MethodPost, "/api/teams/qa/agents", `{"name":"tester","template":"reviewer"}`, http.StatusCreated)
	eventually(t, "tester@qa started", func() bool { return d.Statuses.Get("tester@qa") == runtime.StatusIdle })

	testerBefore, _ := d.Supervisor.Output("tester@qa")

	call(t, d, http.MethodPut, "/api/teams/dev", `{"block":["ghosts"]}`, http.StatusBadRequest)
	call(t, d, http.MethodPut, "/api/teams/dev", `{"block":["dev"]}`, http.StatusBadRequest)

	if team := string(call(t, d, http.MethodPut, "/api/teams/dev", `{"block":["qa"]}`, http.StatusOK)); !strings.Contains(team, `"block":["qa"]`) {
		t.Fatalf("PUT /api/teams/dev = %s", team)
	}

	// Without block, the block list stays (the new goal restarts dev's members,
	// not qa's).
	call(t, d, http.MethodPut, "/api/teams/dev", `{"goal":"Build"}`, http.StatusOK)

	if dev, _ := live.Current().Team("dev"); !slices.Equal(dev.Block, []string{"qa"}) || dev.Goal != "Build" {
		t.Fatalf("dev = %+v, want goal Build and qa still blocked", dev)
	}

	blocked := "Team dev may no longer start conversations with team qa"

	for _, member := range []string{"dev2@dev", "tester@qa"} {
		eventually(t, member+" told of the block", func() bool {
			return strings.Contains(strings.Join(orgNotices(t, d, member), "\n"), blocked)
		})
	}

	if testerAfter, _ := d.Supervisor.Output("tester@qa"); testerAfter != testerBefore {
		t.Fatal("tester@qa restarted: a block change is a notice, not a new session")
	}

	_, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: "dev1@dev", To: "@qa", Act: messaging.ActQuery, Priority: messaging.Normal, Subject: "Ready?",
	})
	if !errors.Is(err, messaging.ErrBlocked) {
		t.Fatalf("dev1@dev -> @qa: %v, want ErrBlocked", err)
	}

	if _, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: "tester@qa", To: "dev1@dev", Act: messaging.ActQuery, Priority: messaging.Normal, Subject: "Ready?",
	}); err != nil {
		t.Fatalf("a block is one-way: tester@qa -> dev1@dev refused: %v", err)
	}

	call(t, d, http.MethodDelete, "/api/teams/qa", "", http.StatusAccepted)

	if dev, _ := live.Current().Team("dev"); len(dev.Block) != 0 {
		t.Fatalf("dev still blocks %v after qa was deleted", dev.Block)
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

// A team created while a feature is active gets its worktree before its
// first agent starts in it. Deleting the team dismisses its members, and
// finishing the feature removes its worktree, although the team is gone.
func TestTeamDuringAFeature(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	dir := live.Current().ProjectDir
	initGitProject(t, dir)

	d := startDaemon(t, live)
	ctx := context.Background()

	f, err := d.Tickets.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Orders"})
	if err != nil {
		t.Fatal(err)
	}

	call(t, d, http.MethodPost, "/api/teams", `{"id":"qa","goal":"Test it"}`, http.StatusCreated)
	call(t, d, http.MethodPost, "/api/teams/qa/agents", `{"name":"tester","template":"qa-engineer"}`, http.StatusCreated)
	eventually(t, "tester@qa started", func() bool { return d.Statuses.Get("tester@qa") == runtime.StatusIdle })

	worktree := d.WorkDir("tester@qa")
	if info, err := os.Stat(worktree); err != nil || !info.IsDir() || worktree == dir {
		t.Fatalf("tester@qa works in %s, which should be the qa team's worktree (%v)", worktree, err)
	}

	call(t, d, http.MethodDelete, "/api/teams/ghosts", "", http.StatusNotFound)
	call(t, d, http.MethodDelete, "/api/teams/qa?now=true", "", http.StatusAccepted)
	eventually(t, "tester@qa dismissed", func() bool { return !hasAgent(t, d, "tester@qa") })

	if _, err := os.Stat(filepath.Join(dir, org.ConfigDirName, "teams", "qa")); !os.IsNotExist(err) {
		t.Fatalf("the team's directory is still there (err %v)", err)
	}

	eventually(t, "dev1@dev told the team was closed", func() bool {
		return strings.Contains(strings.Join(orgNotices(t, d, "dev1@dev"), "\n"), "Team qa was closed")
	})

	if _, err := d.Tickets.FinishFeature(ctx, org.HumanAddress, f.ID, false); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the deleted team's worktree removed", func() bool {
		_, err := os.Stat(worktree)

		return os.IsNotExist(err)
	})
}
