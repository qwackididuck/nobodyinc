package e2e

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/automation"
)

// In automatic mode (docs/automation.md) an agent's permission request is
// allowed by the daemon through the real hook route: no card for the human,
// no agent restart. The mode is reached through the launcher, as the control
// room does.
func TestAutomaticModeAllowsPermissionRequests(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	p := tl.open(t, t.TempDir()+"/proj", true)

	w := tl.attach(t, p.ID, "w1", false)
	w.waitRunning(t)
	tl.directorIdle(t, p.ID)

	var settings struct {
		Address string `json:"address"`
	}
	decodeBody(t, tl.do(t, http.MethodGet, "/p/"+p.ID+"/api/settings", "", http.StatusOK), &settings)

	tl.do(t, http.MethodPut, "/p/"+p.ID+"/api/automation", `{"level":"auto"}`, http.StatusOK)

	body := `{"hook_event_name":"PermissionRequest","session_id":"s-auto","cwd":"/","tool_name":"Bash","tool_input":{"command":"ls"}}`

	// The request is answered at once (this call would block until the human
	// decided otherwise).
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+settings.Address+"/internal/agents/god/hook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	var answer strings.Builder

	buf := make([]byte, 4096)
	n, _ := res.Body.Read(buf)
	answer.Write(buf[:n])
	res.Body.Close()

	if !strings.Contains(answer.String(), `"behavior":"allow"`) {
		t.Fatalf("hook answer = %s, want an allow", answer.String())
	}

	if got := string(tl.do(t, http.MethodGet, "/p/"+p.ID+"/api/approvals", "", http.StatusOK)); strings.Contains(got, `"agent"`) {
		t.Fatalf("a request was queued in automatic mode: %s", got)
	}
}

// In Nobody mode a question to the human is answered by the director: dev1
// asks the human on behalf of a request, the director decides, dev1 gets the
// answer and reports to whoever asked, and the decision waits in the report for
// the human, who acknowledges it. Without the mode, the question stays open.
func TestNobodyModeDirectorAnswersQuestionsToTheHuman(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, budgetOrg(t))

	if _, err := d.Automation.Set(automation.Nobody); err != nil {
		t.Fatal(err)
	}

	sendAsHuman(t, d, "dev1@dev", "Pick a database", "ASK human: Which database for orders?")

	eventually(t, "the decision is reported", func() bool {
		decisions, err := d.Store.ListDecisions(context.Background())

		return err == nil && len(decisions) == 1
	})

	decisions, _ := d.Store.ListDecisions(context.Background())
	if got := decisions[0]; got.AskedBy != "dev1@dev" || got.Decision != "Use SQLite." || got.Impact != automation.ImpactHigh || got.Seen {
		t.Fatalf("decision = %+v", got)
	}

	// dev1 got the answer and told the requester (here, the human).
	want := []string{
		"human -> dev1@dev [request]",
		"dev1@dev -> human [query]",
		"nobody -> god [inform]",
		"god -> dev1@dev [inform]",
		"nobody -> human [inform]",
		"dev1@dev -> human [done]",
	}

	eventually(t, "the full exchange", func() bool { return len(exchange(t, d)) >= len(want) })

	got := exchange(t, d)
	slices.Sort(got)
	slices.Sort(want)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exchange:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
