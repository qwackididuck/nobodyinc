package staffing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/subagents"
)

// writeSubagents gives the test project three subagents: reviewer, linter
// and changelog.
func writeSubagents(t *testing.T, s *Service) {
	t.Helper()

	dir := subagents.Dir(s.Org.Current().ProjectDir)
	writeFile(t, filepath.Join(dir, "reviewer.md"), "---\nname: reviewer\ndescription: Reviews diffs.\ntools: Read, Grep\npermissionMode: acceptEdits\n---\nYou review diffs.\n")
	writeFile(t, filepath.Join(dir, "linter.md"), "---\nname: linter\ndescription: Lints code.\nmodel: haiku\n---\nYou lint.\n")
	writeFile(t, filepath.Join(dir, "changelog.md"), "---\nname: changelog\ndescription: Keeps the changelog.\n---\nYou log.\n")
}

// One import creates a team, imports two subagents (one into the new team,
// one into an existing team), dismisses the third, and starts the new agents
// with a fresh session. Nothing is offered again afterwards.
func TestImportSubagents(t *testing.T) {
	t.Parallel()

	s, a := newService(t)
	writeSubagents(t, s)

	addresses, err := s.ImportSubagents(context.Background(), SubagentImport{
		Teams: []org.Team{{ID: "review", Goal: "Review every change."}},
		Agents: []ImportedAgent{
			{Subagent: "reviewer", Team: "review", Name: "reviewer"},
			{Subagent: "linter", Team: "qa", Name: "lint1", Profile: "allowlist"},
		},
		Ignore: []string{"changelog"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"reviewer@review", "lint1@qa"}; !slices.Equal(addresses, want) {
		t.Fatalf("addresses = %v, want %v", addresses, want)
	}

	o := s.Org.Current()

	if team, ok := o.Team("review"); !ok || team.Goal != "Review every change." {
		t.Fatalf("team review = %+v (found %v)", team, ok)
	}

	reviewer, _ := o.Member("reviewer@review")
	if d := reviewer.Agent.Definition; d.From != "subagent:reviewer" || d.Permissions.Profile != "allowlist" ||
		d.Role != "Reviews diffs" || len(d.Tools) != 0 || !strings.Contains(d.Prompt, "You review diffs.") {
		t.Errorf("reviewer@review = %+v", d)
	}

	if lint, _ := o.Member("lint1@qa"); lint.Agent.Permissions.Profile != "allowlist" || lint.Agent.Model != "haiku" {
		t.Errorf("lint1@qa should get the chosen profile and its model: %+v", lint.Agent.Definition)
	}

	if !slices.Equal(o.IgnoredSubagents, []string{"changelog"}) {
		t.Errorf("ignored subagents = %v", o.IgnoredSubagents)
	}

	for _, address := range addresses {
		if !slices.Contains(a.calls, address+" fresh") {
			t.Errorf("%s should start with a fresh session: %v", address, a.calls)
		}
	}

	all, err := subagents.Load(o.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}

	if pending := subagents.Pending(o, all); len(pending) != 0 {
		t.Errorf("nothing should be pending after the import: %+v", pending)
	}

	if _, err := os.Stat(filepath.Join(subagents.Dir(o.ProjectDir), "reviewer.md")); err != nil {
		t.Errorf("the subagent file must stay: %v", err)
	}
}

// One refused choice writes nothing: neither the team nor the other agents.
func TestImportSubagentsAllOrNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		imp  SubagentImport
		want string
	}{
		{
			name: "an unknown subagent",
			imp:  SubagentImport{Agents: []ImportedAgent{{Subagent: "ghost", Team: "qa", Name: "ghost"}}},
			want: "ghost",
		},
		{
			name: "an invalid agent name",
			imp:  SubagentImport{Agents: []ImportedAgent{{Subagent: "linter", Team: "qa", Name: "Lint_1"}}},
			want: "Lint_1",
		},
		{
			name: "a name already taken",
			imp:  SubagentImport{Agents: []ImportedAgent{{Subagent: "linter", Team: "dev", Name: "dev1"}}},
			want: "dev1@dev",
		},
		{
			name: "two agents with the same address",
			imp: SubagentImport{Agents: []ImportedAgent{
				{Subagent: "linter", Team: "qa", Name: "same"},
				{Subagent: "changelog", Team: "qa", Name: "same"},
			}},
			want: "same@qa",
		},
		{
			name: "a subagent both imported and dismissed",
			imp: SubagentImport{
				Agents: []ImportedAgent{{Subagent: "linter", Team: "qa", Name: "lint"}},
				Ignore: []string{"linter"},
			},
			want: "chosen twice",
		},
		{
			name: "an existing team created again",
			imp:  SubagentImport{Teams: []org.Team{{ID: "qa"}}},
			want: `team "qa"`,
		},
		{
			name: "an unknown team",
			imp:  SubagentImport{Agents: []ImportedAgent{{Subagent: "linter", Team: "nowhere", Name: "lint"}}},
			want: "nowhere",
		},
		{
			name: "auto with a Haiku model",
			imp:  SubagentImport{Agents: []ImportedAgent{{Subagent: "linter", Team: "qa", Name: "lint", Profile: "auto"}}},
			want: "Haiku",
		},
		{
			name: "nothing at all",
			imp:  SubagentImport{},
			want: "nothing to import",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, a := newService(t)
			writeSubagents(t, s)

			// A valid team and agent ride along: they must not be written.
			if tt.name != "nothing at all" {
				tt.imp.Teams = append(tt.imp.Teams, org.Team{ID: "review"})
				tt.imp.Agents = append(tt.imp.Agents, ImportedAgent{Subagent: "reviewer", Team: "review", Name: "reviewer"})
			}

			_, err := s.ImportSubagents(context.Background(), tt.imp)
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want ErrInvalid mentioning %q", err, tt.want)
			}

			o := s.Org.Current()
			if _, exists := o.Team("review"); exists || len(o.IgnoredSubagents) != 0 || a.count() != 0 {
				t.Errorf("a refused import changed something: teams %+v, ignored %v, applied %v", o.Teams, o.IgnoredSubagents, a.calls)
			}

			if _, err := os.Stat(filepath.Join(o.ProjectDir, ".nobody", "teams", "review")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a refused import wrote files: %v", err)
			}
		})
	}
}

// A subagent already imported is no longer offered: importing it again is
// refused.
func TestImportSubagentTwice(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	writeSubagents(t, s)

	imp := SubagentImport{Agents: []ImportedAgent{{Subagent: "linter", Team: "qa", Name: "lint"}}}
	if _, err := s.ImportSubagents(context.Background(), imp); err != nil {
		t.Fatal(err)
	}

	imp.Agents[0].Name = "lint2"
	if _, err := s.ImportSubagents(context.Background(), imp); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("second import = %v, want refused as already imported", err)
	}
}
