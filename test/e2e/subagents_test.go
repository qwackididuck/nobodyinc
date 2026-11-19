package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Importing the project's subagents through the whole daemon (ADR-0018):
//   - the subagents of .claude/agents are listed as pending, with the agent
//     each would become and the tools not carried over;
//   - a refused import writes nothing;
//   - one import creates a team, imports a subagent into it, which starts
//     as an agent, and dismisses another; both are no longer pending, and
//     .claude/agents is untouched.
func TestSubagentsImport(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	dir := live.Current().ProjectDir
	agents := filepath.Join(dir, ".claude", "agents")

	writeTestFile(t, filepath.Join(agents, "reviewer.md"),
		"---\nname: code_reviewer\ndescription: Reviews diffs. Use it after every change.\ntools: Read, Grep\npermissionMode: acceptEdits\n---\nYou review diffs.\n")
	writeTestFile(t, filepath.Join(agents, "changelog.md"), "---\nname: changelog\ndescription: Keeps the changelog.\n---\nYou log.\n")

	d := startDaemon(t, live)

	list := subagentsOf(t, d)
	reviewer := list["code_reviewer"]

	if reviewer.Status != "pending" || reviewer.AgentName != "code-reviewer" || reviewer.DroppedTools != "Read, Grep" ||
		reviewer.Agent.Role != "Reviews diffs" || reviewer.Agent.Permissions.Profile != "allowlist" {
		t.Fatalf("code_reviewer = %+v", reviewer)
	}

	// A refused choice (an invalid agent name) writes nothing, not even the
	// valid team riding along.
	call(t, d, http.MethodPost, "/api/subagents/import",
		`{"teams":[{"id":"review"}],"agents":[{"subagent":"code_reviewer","team":"review","name":"Bad_Name"}]}`, http.StatusBadRequest)

	if _, exists := live.Current().Team("review"); exists {
		t.Fatal("a refused import created its team")
	}

	call(t, d, http.MethodPost, "/api/subagents/import",
		`{"teams":[{"id":"review","goal":"Review every change."}],`+
			`"agents":[{"subagent":"code_reviewer","team":"review","name":"code-reviewer"}],`+
			`"ignore":["changelog"]}`, http.StatusOK)

	eventually(t, "code-reviewer@review started", func() bool {
		_, running := d.Supervisor.Output("code-reviewer@review")

		return running && d.Statuses.Get("code-reviewer@review") == runtime.StatusIdle
	})

	list = subagentsOf(t, d)
	if got := list["code_reviewer"]; got.Status != "imported" || got.ImportedAs != "code-reviewer@review" {
		t.Errorf("code_reviewer after the import = %+v", got)
	}

	if got := list["changelog"]; got.Status != "ignored" {
		t.Errorf("changelog after the import = %+v", got)
	}

	if data, err := os.ReadFile(filepath.Join(agents, "reviewer.md")); err != nil || !strings.Contains(string(data), "name: code_reviewer") {
		t.Errorf(".claude/agents must stay untouched: %s, %v", data, err)
	}

	// Importing it again is refused: it is no longer pending.
	call(t, d, http.MethodPost, "/api/subagents/import",
		`{"agents":[{"subagent":"code_reviewer","team":"review","name":"another"}]}`, http.StatusBadRequest)
}

// listedSubagent is an entry of GET /api/subagents.
type listedSubagent struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	ImportedAs   string `json:"imported_as"`
	AgentName    string `json:"agent_name"`
	DroppedTools string `json:"dropped_tools"`
	Agent        struct {
		Role        string `json:"role"`
		Permissions struct {
			Profile string `json:"profile"`
		} `json:"permissions"`
	} `json:"agent"`
}

// subagentsOf returns GET /api/subagents, by name.
func subagentsOf(t *testing.T, d *daemon.Daemon) map[string]listedSubagent {
	t.Helper()

	var list struct {
		Subagents []listedSubagent `json:"subagents"`
	}
	if err := json.Unmarshal(call(t, d, http.MethodGet, "/api/subagents", "", http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}

	byName := map[string]listedSubagent{}
	for _, s := range list.Subagents {
		byName[s.Name] = s
	}

	return byName
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
