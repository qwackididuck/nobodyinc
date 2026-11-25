package e2e

import (
	"net/http"
	"strings"
	"testing"
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
