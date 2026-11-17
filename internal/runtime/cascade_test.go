package runtime

import (
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// The system prompt goes from the general to the particular, and leaves out
// the sections that have nothing to say.
func TestSystemPromptCascade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		user    string
		project string
		team    string
		// want appear in this order; absent must not appear.
		want   []string
		absent []string
	}{
		{
			name:    "every level",
			user:    "Answer in English.",
			project: "A shop in Go.",
			team:    "Use TDD.",
			want: []string{
				"## Your user's instructions", "the rules of Nobody Inc win", "Answer in English.",
				"## The project", "A shop in Go.",
				"## Your team: dev", "Team goal: Build it", "Use TDD.",
				"## Your role: Developer", "Write code.",
				"### Team specs", "archi@specs (Architect) — ask them about: the design",
				"## Whom to ask", "**Decide yourself**", "**Ask the human**", "The director is not a source of knowledge",
			},
		},
		{
			name:   "no shared prompt",
			want:   []string{"## Your team: dev", "## Your role: Developer"},
			absent: []string{"## Your user's instructions", "## The project", "What every member of your team follows"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := testOrg()
			o.ProjectPrompt = tt.project
			o.Teams[0].Agents[0].Expertise = "the design"
			o.Teams[1].Prompt = tt.team
			o.Teams[1].Agents[0].Prompt = "Write code."
			m, _ := o.Member("dev1@dev")

			got, err := SystemPrompt(o, m, tt.user)
			if err != nil {
				t.Fatal(err)
			}

			rest := got
			for _, want := range tt.want {
				i := strings.Index(rest, want)
				if i < 0 {
					t.Fatalf("missing, or out of order: %q in\n%s", want, got)
				}

				rest = rest[i+len(want):]
			}

			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("should not contain %q:\n%s", absent, got)
				}
			}
		})
	}
}

func TestDirectorGetsItsOwnRoutingRule(t *testing.T) {
	t.Parallel()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director"}
	god, _ := o.Member("god")

	got, err := SystemPrompt(o, god, "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You know what the human wants, not how the project works") || strings.Contains(got, "**Decide yourself**") {
		t.Fatalf("director's routing rule:\n%s", got)
	}

	if !strings.Contains(got, "`team_create`") {
		t.Fatal("the director is not told it can create teams")
	}
}
