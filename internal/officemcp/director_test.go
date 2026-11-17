package officemcp

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// fakeDirector records the changes the director's tools ask for.
type fakeDirector struct {
	calls []string
}

func (f *fakeDirector) Overview(context.Context) (Overview, error) {
	return Overview{Teams: []TeamOverview{{Team: "dev"}}}, nil
}

func (f *fakeDirector) Templates() ([]catalog.Template, error) {
	return []catalog.Template{{Name: "dev"}}, nil
}

func (f *fakeDirector) CreateTeam(_ context.Context, id, goal, prompt string) (org.Team, error) {
	f.calls = append(f.calls, "team "+id+": "+goal+" / "+prompt)

	return org.Team{ID: id, Goal: goal, Prompt: prompt}, nil
}

func (f *fakeDirector) DeleteTeam(_ context.Context, id string) error {
	f.calls = append(f.calls, "delete team "+id)

	return nil
}

func (f *fakeDirector) Definition(string) (catalog.Definition, error) {
	return catalog.Definition{Role: "Developer"}, nil
}

func (f *fakeDirector) Recruit(_ context.Context, team, name, template string) (org.Member, error) {
	f.calls = append(f.calls, "recruit "+name+"@"+team+" from "+template)

	return org.Member{Address: org.Address(name, team)}, nil
}

func (f *fakeDirector) Create(_ context.Context, team, name string, _ catalog.Definition) (org.Member, error) {
	f.calls = append(f.calls, "create "+name+"@"+team)

	return org.Member{Address: org.Address(name, team)}, nil
}

func (f *fakeDirector) Edit(_ context.Context, address string, _ catalog.Definition, _ bool) (catalog.Definition, error) {
	f.calls = append(f.calls, "edit "+address)

	return catalog.Definition{}, nil
}

func (f *fakeDirector) Leave(_ context.Context, address string) error {
	f.calls = append(f.calls, "leave "+address)

	return nil
}

func (f *fakeDirector) Return(_ context.Context, address string) error {
	f.calls = append(f.calls, "return "+address)

	return nil
}

func (f *fakeDirector) Dismiss(_ context.Context, address string) error {
	f.calls = append(f.calls, "dismiss "+address)

	return nil
}

func connectDirector(t *testing.T, address string, director Director) *mcp.ClientSession {
	t.Helper()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director"}
	sender := &messaging.Service{Store: memStore{}, Policy: messaging.Policy{Org: o}}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := NewServer(o, Office{Sender: sender, Director: director, Version: "test"}, address).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}

	return names
}

func TestOnlyTheDirectorGetsDirectorTools(t *testing.T) {
	t.Parallel()

	god := toolNames(t, connectDirector(t, "god", &fakeDirector{}))
	for _, name := range directorToolNames {
		if !slices.Contains(god, name) {
			t.Errorf("the director misses %s", name)
		}
	}

	dev := toolNames(t, connectDirector(t, "dev1@dev", &fakeDirector{}))
	for _, name := range directorToolNames {
		if slices.Contains(dev, name) {
			t.Errorf("a team agent got the director's tool %s", name)
		}
	}

	for _, name := range directorToolNames {
		if !slices.Contains(ToolNames(), name) {
			t.Errorf("%s is not allowed without asking (ToolNames)", name)
		}
	}
}

func TestDirectorToolsCallTheDirector(t *testing.T) {
	t.Parallel()

	director := &fakeDirector{}
	session := connectDirector(t, "god", director)

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"team_create", map[string]any{"id": "qa", "goal": "Test it", "prompt": "Prove the tests can fail."}},
		{"agent_recruit", map[string]any{"team": "dev", "name": "dev2", "template": "developer"}},
		{"agent_create", map[string]any{"team": "qa", "name": "fuzzer", "definition": map[string]any{"role": "Fuzzer"}}},
		{"agent_edit", map[string]any{"address": "dev1@dev", "definition": map[string]any{"role": "Lead"}}},
		{"agent_leave", map[string]any{"address": "dev1@dev"}},
		{"agent_return", map[string]any{"address": "dev1@dev"}},
		{"agent_dismiss", map[string]any{"address": "tester@qa"}},
		{"team_delete", map[string]any{"id": "qa"}},
		{"org_overview", nil},
		{"template_list", nil},
		{"agent_get", map[string]any{"address": "dev1@dev"}},
	}

	for _, c := range calls {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil || res.IsError {
			t.Fatalf("%s: err=%v result=%s", c.tool, err, textOf(res))
		}
	}

	want := "team qa: Test it / Prove the tests can fail. | recruit dev2@dev from developer | create fuzzer@qa | edit dev1@dev | leave dev1@dev | return dev1@dev | dismiss tester@qa | delete team qa"
	if got := strings.Join(director.calls, " | "); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestDirectorCannotChangeItself(t *testing.T) {
	t.Parallel()

	director := &fakeDirector{}
	session := connectDirector(t, "god", director)

	for _, tool := range []string{"agent_edit", "agent_leave", "agent_dismiss"} {
		args := map[string]any{"address": "god"}
		if tool == "agent_edit" {
			args["definition"] = map[string]any{"role": "God of everything"}
		}

		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}

		if !res.IsError || !strings.Contains(textOf(res), errSelf.Error()) {
			t.Errorf("%s on itself: %s, want a refusal", tool, textOf(res))
		}
	}

	if len(director.calls) != 0 {
		t.Fatalf("the director changed itself: %v", director.calls)
	}
}
