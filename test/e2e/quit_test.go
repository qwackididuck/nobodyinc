package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// quitBound is far above what stopping a project takes (milliseconds), and
// far below the 10 seconds a project daemon's graceful shutdown waits for a
// request still open.
const quitBound = 5 * time.Second

// Quitting the app stops every project daemon, on the app's main thread, with
// its window frozen (docs/desktop.md). An agent waiting for the human's
// approval holds a request open on its project daemon (its PermissionRequest
// hook, not through the launcher): the daemon must abandon it when it is told
// to stop, as after a restart (the agent falls back to its terminal), not wait
// for it until the HTTP server's shutdown times out.
func TestLauncherStopsAtOnceWithAPendingApproval(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	p := tl.open(t, t.TempDir()+"/proj", true)

	w := tl.attach(t, p.ID, "w1", false)
	w.waitRunning(t)
	tl.directorIdle(t, p.ID)

	// The daemon's own address: the agents' hooks do not go through the launcher.
	var settings struct {
		Address string `json:"address"`
	}
	decodeBody(t, tl.do(t, http.MethodGet, "/p/"+p.ID+"/api/settings", "", http.StatusOK), &settings)

	body := `{"hook_event_name":"PermissionRequest","session_id":"s-quit","cwd":"/","tool_name":"Bash","tool_input":{"command":"ls"}}`

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+settings.Address+"/internal/agents/god/hook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	answered := make(chan struct{})

	go func() {
		defer close(answered)

		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}()

	eventually(t, "the permission request is waiting for the human", func() bool {
		return strings.Contains(string(tl.do(t, http.MethodGet, "/p/"+p.ID+"/api/approvals", "", http.StatusOK)), `"agent":"god"`)
	})

	start := time.Now()

	tl.launcher.Close()

	if took := time.Since(start); took > quitBound {
		t.Fatalf("stopping the project took %s with a permission request pending, want well under %s", took, quitBound)
	}

	select {
	case <-answered:
	case <-time.After(quitBound):
		t.Fatal("the agent's hook was left waiting after the project stopped")
	}
}
