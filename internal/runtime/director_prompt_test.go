package runtime

import (
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func TestDirectorInTheSystemPrompts(t *testing.T) {
	t.Parallel()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director", Prompt: "Lead the teams."}

	god, _ := o.Member("god")

	godPrompt, err := SystemPrompt(o, god, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"You are the director", "Lead the teams.", "### Team specs", "### Team dev", "You can write to anyone in this team"} {
		if !strings.Contains(godPrompt, want) {
			t.Errorf("director's prompt misses %q:\n%s", want, godPrompt)
		}
	}

	if strings.Contains(godPrompt, "## Your team") || strings.Contains(godPrompt, "### The director") {
		t.Errorf("the director has no team and is not its own contact:\n%s", godPrompt)
	}

	dev, _ := o.Member("dev1@dev")

	devPrompt, err := SystemPrompt(o, dev, "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(devPrompt, "### The director") || !strings.Contains(devPrompt, "- god (Director)") {
		t.Errorf("an agent's prompt should present the director:\n%s", devPrompt)
	}
}
