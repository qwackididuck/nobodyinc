package e2e

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Commands through the whole daemon (ADR-0017):
//   - a project command created through the API, in a sub-directory, is
//     written to .claude/commands, and every agent restarts to get it;
//   - it is listed with its allowed-tools, and read back with the header
//     keys the control room does not edit;
//   - an invalid name is refused; deleting it removes the file and the
//     sub-directory it leaves empty, and agents restart again.
//
// Personal commands are left out on purpose, as personal skills are
// (TestSkillsReachAgents): unit tests cover them (internal/commands,
// internal/runtime, internal/staffing).
func TestCommandsReachAgents(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	dir := live.Current().ProjectDir
	file := filepath.Join(dir, ".claude", "commands", "front", "test.md")

	waitRestart := func(what string, change func()) {
		t.Helper()

		before, _ := d.Supervisor.Output("dev2@dev")

		change()

		eventually(t, what, func() bool {
			after, running := d.Supervisor.Output("dev2@dev")

			return running && after != before && d.Statuses.Get("dev2@dev") == runtime.StatusIdle
		})
	}

	waitRestart("dev2@dev restarted for the new command", func() {
		call(t, d, http.MethodPut, "/api/commands/project/front:test", `{"description":"Runs the front tests.","body":"Run pnpm test $ARGUMENTS."}`, http.StatusOK)
	})

	if data, err := os.ReadFile(file); err != nil || !strings.Contains(string(data), "Run pnpm test $ARGUMENTS.") {
		t.Fatalf("command file = %s, %v", data, err)
	}

	// A header key the control room does not edit, added by hand, is shown
	// and kept by the next save.
	if err := os.WriteFile(file, []byte("---\ndescription: Runs the front tests.\nallowed-tools: Bash(pnpm test *)\n---\nRun pnpm test.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	call(t, d, http.MethodPut, "/api/commands/project/front:test", `{"description":"Runs the front tests.","body":"Run pnpm test, then lint."}`, http.StatusOK)

	// The project is not a git repository: whether the command is committed
	// is not told (internal/commands tests it).
	listed := commandNamed(t, d, "front:test")
	if listed.Scope != "project" || listed.AllowedTools != "Bash(pnpm test *)" || listed.Committed != nil {
		t.Fatalf("listed command = %+v, want project, allowed tools, committed untold", listed)
	}

	var detail struct {
		Body      string `json:"body"`
		OtherKeys string `json:"other_keys"`
	}
	if err := json.Unmarshal(call(t, d, http.MethodGet, "/api/commands/project/front:test", "", http.StatusOK), &detail); err != nil {
		t.Fatal(err)
	}

	if detail.Body != "Run pnpm test, then lint." || detail.OtherKeys != "allowed-tools: Bash(pnpm test *)\n" {
		t.Fatalf("command detail = %+v", detail)
	}

	call(t, d, http.MethodPut, "/api/commands/project/Bad_Name", `{"description":"","body":"x"}`, http.StatusBadRequest)
	call(t, d, http.MethodGet, "/api/commands/project/ghost", "", http.StatusNotFound)

	waitRestart("dev2@dev restarted once the command is gone", func() {
		call(t, d, http.MethodDelete, "/api/commands/project/front:test", "", http.StatusNoContent)
	})

	if _, err := os.Stat(filepath.Dir(file)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the emptied sub-directory should be gone: %v", err)
	}
}

// listedCommand is an entry of GET /api/commands.
type listedCommand struct {
	Name         string `json:"name"`
	Scope        string `json:"scope"`
	AllowedTools string `json:"allowed_tools"`
	Committed    *bool  `json:"committed"`
}

// commandNamed returns the project command of that name from GET
// /api/commands.
func commandNamed(t *testing.T, d *daemon.Daemon, name string) listedCommand {
	t.Helper()

	var list struct {
		Commands []listedCommand `json:"commands"`
	}
	if err := json.Unmarshal(call(t, d, http.MethodGet, "/api/commands", "", http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}

	for _, c := range list.Commands {
		if c.Name == name && c.Scope == "project" {
			return c
		}
	}

	t.Fatalf("no project command %s in %+v", name, list.Commands)

	return listedCommand{}
}
