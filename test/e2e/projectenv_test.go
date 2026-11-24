package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// A project's environment variable set through the API reaches its agents:
// each restarts once idle and has it in its own process; deleting it takes it
// away. The value is never returned by the API (ADR-0020).
func TestProjectEnvironmentReachesTheAgents(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	envFile := filepath.Join(t.TempDir(), "env.yaml")
	d := startDaemonWithOptions(t, live, ":memory:", func(o *daemon.Options) { o.EnvFile = envFile })

	if got := ask(t, d, "dev1@dev", "ENV? PROJECT_TOKEN"); got != "unset" {
		t.Fatalf("before setting it, PROJECT_TOKEN = %q", got)
	}

	restarted := func(address string, before *runtime.Output) func() bool {
		return func() bool {
			after, running := d.Supervisor.Output(address)

			return running && after != before && d.Statuses.Get(address) == runtime.StatusIdle
		}
	}

	agents := []string{"dev1@dev", "dev2@dev"}
	outputs := func() map[string]*runtime.Output {
		m := map[string]*runtime.Output{}
		for _, a := range agents {
			m[a], _ = d.Supervisor.Output(a)
		}

		return m
	}

	before := outputs()

	list := string(call(t, d, http.MethodPut, "/api/env/PROJECT_TOKEN", `{"value":"s3cret"}`, http.StatusOK))
	if strings.Contains(list, "s3cret") || !strings.Contains(list, `"PROJECT_TOKEN"`) {
		t.Fatalf("PUT /api/env = %s, want the name and not the value", list)
	}

	for _, a := range agents {
		eventually(t, a+" restarted with the variable", restarted(a, before[a]))

		if got := ask(t, d, a, "ENV? PROJECT_TOKEN"); got != "s3cret" {
			t.Fatalf("PROJECT_TOKEN in %s's process = %q, want the project's value", a, got)
		}
	}

	// The daemon's own environment, hence the user's shell, is untouched.
	if _, set := os.LookupEnv("PROJECT_TOKEN"); set {
		t.Fatal("the project's variable leaked into the daemon's process")
	}

	data, err := os.ReadFile(envFile)
	if err != nil || !strings.Contains(string(data), "s3cret") {
		t.Fatalf("env file = %q, %v", data, err)
	}

	call(t, d, http.MethodPut, "/api/env/NOBODY_AGENT", `{"value":"someone@else"}`, http.StatusBadRequest)

	before = outputs()

	call(t, d, http.MethodDelete, "/api/env/PROJECT_TOKEN", "", http.StatusNoContent)

	for _, a := range agents {
		eventually(t, a+" restarted without the variable", restarted(a, before[a]))

		if got := ask(t, d, a, "ENV? PROJECT_TOKEN"); got != "unset" {
			t.Fatalf("after deleting it, PROJECT_TOKEN in %s = %q", a, got)
		}
	}
}
