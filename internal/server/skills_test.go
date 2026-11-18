package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// A SessionStart hook event gets the skills the agent preloads, read in the
// directory it works in (ADR-0011); an agent preloading nothing gets nothing.
func TestSessionStartHandsOverPreloadedSkills(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	skillDir := filepath.Join(project, ".claude", "skills", "review")

	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: R.\n---\nCheck the tests.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := &org.Org{ProjectDir: project, Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
		{Name: "dev1", Definition: catalog.Definition{Skills: []string{"review"}}}, //nolint:modernize // the definition reads better as a whole
		{Name: "dev2"},
	}}}}
	env := testEnv{handler: New(Deps{Logger: slog.New(slog.DiscardHandler), Org: o, Store: st})}

	var decision hookDecision
	decode(t, env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook",
		`{"hook_event_name":"SessionStart","source":"resume","cwd":"`+project+`"}`), &decision)

	if decision.Response == nil || decision.Response.HookSpecificOutput == nil ||
		!strings.Contains(decision.Response.HookSpecificOutput.AdditionalContext, "Check the tests.") {
		t.Fatalf("decision = %+v, want the review skill", decision)
	}

	decision = hookDecision{}
	decode(t, env.do(t, http.MethodPost, "/internal/agents/dev2@dev/hook", `{"hook_event_name":"SessionStart","source":"startup"}`), &decision)

	if decision.Response != nil || decision.MessageIDs == nil {
		t.Fatalf("dev2 preloads nothing, got %+v", decision)
	}
}
