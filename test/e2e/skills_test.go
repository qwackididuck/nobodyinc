package e2e

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Skills through the whole daemon (ADR-0011):
//   - a project skill created through the API is written to .claude/skills,
//     and every agent restarts to get it;
//   - an agent told to preload it restarts resuming its session, and its
//     SessionStart hook hands the skill over (its exclusions go into its
//     launch flags: internal/runtime tests that); a skill both preloaded and
//     excluded is refused;
//   - the built-in skills are discovered from claude (here fakeclaude);
//   - a changed skill reaches the agent preloading it on its next resume.
//
// Personal skills are left out on purpose: they live in the CLAUDE_CONFIG_DIR
// shared by the tests running in parallel (TestMain), and every project sees
// them. Unit tests cover them (internal/skills, internal/runtime).
func TestSkillsReachAgents(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	dir := live.Current().ProjectDir

	before, _ := d.Supervisor.Output("dev2@dev")

	call(t, d, http.MethodPut, "/api/skills/project/review", `{"description":"Reviews a change.","body":"Check the tests first."}`, http.StatusOK)

	if data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "review", "SKILL.md")); err != nil || !strings.Contains(string(data), "Check the tests first.") {
		t.Fatalf("SKILL.md = %s, %v", data, err)
	}

	eventually(t, "dev2@dev restarted for the new skill", func() bool {
		after, running := d.Supervisor.Output("dev2@dev")

		return running && after != before && d.Statuses.Get("dev2@dev") == runtime.StatusIdle
	})

	before, _ = d.Supervisor.Output("dev1@dev")
	call(t, d, http.MethodPut, "/api/agents/dev1@dev/definition",
		`{"role":"Developer","skills":["review"],"skills_exclude":["fake-builtin"]}`, http.StatusOK)
	call(t, d, http.MethodPut, "/api/agents/dev1@dev/definition",
		`{"role":"Developer","skills":["review"],"skills_exclude":["review"]}`, http.StatusBadRequest)

	eventually(t, "dev1@dev restarted with its skills", func() bool {
		after, running := d.Supervisor.Output("dev1@dev")

		return running && after != before && d.Statuses.Get("dev1@dev") == runtime.StatusIdle
	})

	preloaded := askPreloaded(t, d, "dev1@dev")
	if !strings.Contains(preloaded, "source=resume") || !strings.Contains(preloaded, `<preloaded_skill name="review"`) ||
		!strings.Contains(preloaded, "Check the tests first.") {
		t.Fatalf("dev1@dev should have resumed with the review skill, got:\n%s", preloaded)
	}

	if !strings.Contains(askPreloaded(t, d, "dev2@dev"), "source=resume\n") || strings.Contains(askPreloaded(t, d, "dev2@dev"), "preloaded_skill") {
		t.Fatal("dev2@dev preloads nothing")
	}

	eventually(t, "the built-in skills discovered", func() bool {
		return strings.Contains(string(call(t, d, http.MethodGet, "/api/skills", "", http.StatusOK)), `"name":"fake-builtin","description":"","scope":"builtin"`)
	})

	// A changed skill: the agent resumes and gets the new version.
	call(t, d, http.MethodPut, "/api/skills/project/review", `{"description":"Reviews a change.","body":"Check the docs too."}`, http.StatusOK)

	eventually(t, "dev1@dev got the new version of review", func() bool {
		return d.Statuses.Get("dev1@dev") == runtime.StatusIdle && strings.Contains(askPreloaded(t, d, "dev1@dev"), "Check the docs too.")
	})
}

// askPreloaded asks an agent (fakeclaude) what its SessionStart hook handed
// over, and waits for the answer.
func askPreloaded(t *testing.T, d *daemon.Daemon, address string) string {
	t.Helper()

	sent, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: address, Act: messaging.ActQuery, Priority: messaging.Normal,
		Subject: "Skills", Body: "PRELOADED SKILLS?",
	})
	if err != nil {
		t.Fatal(err)
	}

	return waitForAnswer(t, d, sent.ID).Body
}
