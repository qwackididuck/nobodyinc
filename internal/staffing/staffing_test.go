package staffing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// applied records what the service asked the supervisor to apply.
type applied struct {
	mu    sync.Mutex
	calls []string // "<address> fresh=<bool> now=<bool>"
}

func (a *applied) Apply(_ context.Context, address string, opts runtime.ApplyOptions) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	call := address
	if opts.Fresh {
		call += " fresh"
	}

	if opts.Now {
		call += " now"
	}

	a.calls = append(a.calls, call)

	return nil
}

func (a *applied) last() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.calls) == 0 {
		return ""
	}

	return a.calls[len(a.calls)-1]
}

func (a *applied) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return len(a.calls)
}

// newService: a project with team dev (dev1) and team qa (no one), an MCP
// registry with gitlab, and an empty user catalog.
func newService(t *testing.T) (*Service, *applied) {
	t.Helper()

	dir := t.TempDir()

	o := &org.Org{ProjectDir: dir, Teams: []org.Team{
		{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer", From: "dev@1"}}},
		{ID: "qa"},
	}}
	if err := org.WriteProject(o); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, ".nobody", "mcp.yaml"), "servers:\n  gitlab: {command: gitlab-mcp}\n")

	loaded, err := org.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	a := &applied{}

	return &Service{Org: org.NewLive(loaded), Catalog: catalog.New(t.TempDir()), Agents: a}, a
}

func TestRecruit(t *testing.T) {
	t.Parallel()

	s, a := newService(t)

	m, err := s.Recruit(context.Background(), "qa", "tester", "reviewer")
	if err != nil {
		t.Fatal(err)
	}

	if m.Address != "tester@qa" || !strings.HasPrefix(m.Agent.From, "reviewer@") || m.Agent.Description != "" {
		t.Fatalf("recruit = %+v", m)
	}

	if a.last() != "tester@qa fresh" {
		t.Fatalf("applied %q, want a fresh start of tester@qa", a.last())
	}

	// On disk, and in the reloaded organization.
	reloaded, err := org.LoadProject(s.Org.Current().ProjectDir)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := reloaded.Member("tester@qa"); !ok {
		t.Fatal("tester@qa not written to .nobody/")
	}
}

func TestRecruitRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, team, agent, template string
		want                        error
	}{
		{name: "agent exists", team: "dev", agent: "dev1", template: "dev", want: ErrExists},
		{name: "unknown team", team: "ghosts", agent: "a", template: "dev", want: ErrNotFound},
		{name: "unknown template", team: "dev", agent: "a", template: "astronaut", want: ErrNotFound},
		{name: "invalid name", team: "dev", agent: "Dev 2", template: "dev", want: ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, a := newService(t)

			if _, err := s.Recruit(context.Background(), tt.team, tt.agent, tt.template); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			if a.count() != 0 {
				t.Fatal("a refused recruitment must not start anything")
			}
		})
	}
}

func TestCreateWritesNothingInvalid(t *testing.T) {
	t.Parallel()

	s, a := newService(t)

	_, err := s.Create(context.Background(), "dev", "dev2", catalog.Definition{MCP: []string{"ragflow"}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid (ragflow is not in mcp.yaml)", err)
	}

	if _, statErr := os.Stat(filepath.Join(s.Org.Current().ProjectDir, ".nobody", "teams", "dev", "dev2.md")); statErr == nil {
		t.Fatal("an invalid agent was written to disk")
	}

	m, err := s.Create(context.Background(), "dev", "dev2", catalog.Definition{Role: "Developer", MCP: []string{"gitlab"}, From: "forged@9"})
	if err != nil {
		t.Fatal(err)
	}

	if m.Agent.From != "" || a.last() != "dev2@dev fresh" {
		t.Fatalf("created %+v, applied %q", m.Agent, a.last())
	}
}

func TestEditKeepsOriginAndLeave(t *testing.T) {
	t.Parallel()

	s, a := newService(t)

	saved, err := s.Edit(context.Background(), "dev1@dev", catalog.Definition{
		Role: "Lead developer", Description: "template only", Version: 4, From: "forged@9", OnLeave: true,
	}, runtime.ApplyOptions{Now: true})
	if err != nil {
		t.Fatal(err)
	}

	if saved.Role != "Lead developer" || saved.From != "dev@1" || saved.OnLeave || saved.Description != "" || saved.Version != 0 {
		t.Fatalf("saved = %+v", saved)
	}

	if a.last() != "dev1@dev now" {
		t.Fatalf("applied %q, want dev1@dev resumed now", a.last())
	}

	if d, _ := s.Definition("dev1@dev"); d.Role != "Lead developer" {
		t.Fatalf("definition after edit = %+v", d)
	}

	if _, err := s.Edit(context.Background(), "ghost@dev", catalog.Definition{}, runtime.ApplyOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit unknown agent: err = %v", err)
	}
}

func TestLeaveReturnDismiss(t *testing.T) {
	t.Parallel()

	s, a := newService(t)
	ctx := context.Background()

	if err := s.Leave(ctx, "dev1@dev", runtime.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	if d, _ := s.Definition("dev1@dev"); !d.OnLeave || a.last() != "dev1@dev" {
		t.Fatalf("after leave: on_leave=%v applied=%q", d.OnLeave, a.last())
	}

	calls := a.count()
	if err := s.Leave(ctx, "dev1@dev", runtime.ApplyOptions{}); err != nil || a.count() != calls {
		t.Fatalf("a second leave should do nothing: err=%v", err)
	}

	if err := s.Return(ctx, "dev1@dev", runtime.ApplyOptions{Fresh: true}); err != nil {
		t.Fatal(err)
	}

	if d, _ := s.Definition("dev1@dev"); d.OnLeave || a.last() != "dev1@dev fresh" {
		t.Fatalf("after return: on_leave=%v applied=%q", d.OnLeave, a.last())
	}

	if err := s.Dismiss(ctx, "dev1@dev", runtime.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Definition("dev1@dev"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismissed agent still there: %v", err)
	}

	if a.last() != "dev1@dev" {
		t.Fatalf("applied %q, want dev1@dev stopped", a.last())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
