package e2e

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// The project's knowledge base through the whole daemon (ADR-0019):
//   - without one, the agents do not get the knowledge tools, and the API
//     says there is none;
//   - a path the rules refuse is refused, and nothing is written;
//   - setting docs/ up writes org.yaml, announces kb.changed, restarts every
//     agent with the knowledge tools, and serves the tree, a page with its
//     backlinks and broken links, and the search;
//   - an agent's kb_search finds a page, and the office is told (kb.read);
//   - a page written by hand is picked up by the background scan;
//   - removing the knowledge base leaves the files and takes the tools away.
func TestKnowledgeBase(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	dir := live.Current().ProjectDir

	for name, content := range map[string]string{
		"README.md":           "# Docs\n\nRead [auth](kb/auth.md) and [ADR-0003](adr/0003-isolate.md).\n",
		"adr/0003-isolate.md": "# ADR-0003 Isolate agents\n\nAgents never see the user's settings.\n",
		"kb/auth.md":          "# Authentication\n\nSee [refresh](refresh.md) and [old design](../adr/0099.md).\n",
		"kb/refresh.md":       "# Token refresh\n\nAsk for a new token before it expires.\n",
	} {
		writeTestFile(t, filepath.Join(dir, "docs", filepath.FromSlash(name)), content)
	}

	d := startDaemonWithOptions(t, live, ":memory:", func(o *daemon.Options) { o.KnowledgeScanEvery = 100 * time.Millisecond })
	stream := subscribe(t, d.Handler)

	// No knowledge base yet: no tool for the agents.
	if got := string(call(t, d, http.MethodGet, "/api/kb", "", http.StatusOK)); !strings.Contains(got, `"path":""`) {
		t.Fatalf("GET /api/kb without a knowledge base = %s", got)
	}

	call(t, d, http.MethodGet, "/api/kb/tree", "", http.StatusNotFound)

	if answer := askKnowledge(t, d, "expires"); strings.Contains(answer, "kb/refresh.md") {
		t.Fatalf("an agent searched a knowledge base the project does not have: %s", answer)
	}

	// Refused paths write nothing.
	for _, bad := range []string{`{"path":"../elsewhere"}`, `{"path":".nobody"}`, `{"path":"/etc"}`} {
		call(t, d, http.MethodPut, "/api/kb", bad, http.StatusBadRequest)
	}

	if live.Current().KnowledgePath() != "" {
		t.Fatal("a refused path was saved")
	}

	before := outputs(d, live.Current())

	overview := string(call(t, d, http.MethodPut, "/api/kb", `{"path":"docs/"}`, http.StatusOK))
	for _, want := range []string{`"path":"docs"`, `"exists":true`, `"pages":4`, `"home":"README.md"`, `"target":"../adr/0099.md"`} {
		if !strings.Contains(overview, want) {
			t.Errorf("PUT /api/kb answer lacks %s: %s", want, overview)
		}
	}

	waitForEvent(t, stream, "kb.changed", func(data string) bool { return strings.Contains(data, `"reset":true`) })

	if data, err := os.ReadFile(filepath.Join(dir, org.ConfigDirName, "org.yaml")); err != nil || !strings.Contains(string(data), "path: docs") {
		t.Fatalf("org.yaml = %s, %v", data, err)
	}

	// Every agent restarts once, resuming its session, to get the tools.
	waitRestarted(t, d, live.Current(), before)

	tree := string(call(t, d, http.MethodGet, "/api/kb/tree", "", http.StatusOK))
	if !strings.Contains(tree, `"title":"Token refresh"`) || !strings.Contains(tree, `"name":"adr"`) {
		t.Errorf("GET /api/kb/tree = %s", tree)
	}

	page := string(call(t, d, http.MethodGet, "/api/kb/pages/kb/auth.md", "", http.StatusOK))
	for _, want := range []string{`"title":"Authentication"`, `"backlinks":[{"path":"README.md","title":"Docs"}]`, `kb-broken`} {
		if !strings.Contains(page, want) {
			t.Errorf("GET /api/kb/pages/kb/auth.md lacks %s: %s", want, page)
		}
	}

	if found := string(call(t, d, http.MethodGet, "/api/kb/search?q=settings", "", http.StatusOK)); !strings.Contains(found, `"path":"adr/0003-isolate.md"`) {
		t.Errorf("GET /api/kb/search = %s", found)
	}

	// An agent searches the knowledge base with its own tool; the office
	// walks it to the library.
	if answer := askKnowledge(t, d, "expires"); !strings.Contains(answer, `"path":"kb/refresh.md"`) || !strings.Contains(answer, "«expires»") {
		t.Fatalf("kb_search answer = %s", answer)
	}

	waitForEvent(t, stream, "kb.read", func(data string) bool {
		return strings.Contains(data, `"agent":"dev1@dev"`) && strings.Contains(data, `"tool":"kb_search"`)
	})

	// A page written by hand (or by an agent) is picked up in the background.
	writeTestFile(t, filepath.Join(dir, "docs", "kb", "glossary.md"), "# Glossary\n")
	waitForEvent(t, stream, "kb.changed", func(data string) bool { return strings.Contains(data, `"added":1`) })

	// Removing the knowledge base keeps the files.
	before = outputs(d, live.Current())

	call(t, d, http.MethodPut, "/api/kb", `{"path":""}`, http.StatusOK)
	waitRestarted(t, d, live.Current(), before)
	call(t, d, http.MethodGet, "/api/kb/tree", "", http.StatusNotFound)

	if _, err := os.Stat(filepath.Join(dir, "docs", "kb", "glossary.md")); err != nil {
		t.Errorf("removing the knowledge base touched its files: %v", err)
	}
}

// askKnowledge asks dev1@dev "KB? <words>": fakeclaude answers with what its
// kb_search tool returns (or why it could not call it).
func askKnowledge(t *testing.T, d *daemon.Daemon, words string) string {
	t.Helper()

	question, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: "dev1@dev", Act: messaging.ActQuery, Priority: messaging.Normal,
		Subject: "Docs", Body: "KB? " + words,
	})
	if err != nil {
		t.Fatal(err)
	}

	return waitForAnswer(t, d, question.ID).Body
}
