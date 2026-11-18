package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// An MCP server added for the whole project through the API is written by
// Claude Code (here fakeclaude) to the project's .mcp.json, with its secret
// left as a variable; every agent restarts to get it. A secret in clear is
// refused, and the server can be removed again (ADR-0010).
func TestMCPServerAddedForTheProject(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	dir := live.Current().ProjectDir

	before, _ := d.Supervisor.Output("dev1@dev")

	server := `{"name":"ragflow","scope":"project","server":{"type":"http","url":"http://localhost:9380/mcp",` +
		`"headers":{"Authorization":"Bearer ${RAGFLOW_KEY}"}}}`
	if list := string(call(t, d, http.MethodPost, "/api/mcp", server, http.StatusCreated)); !strings.Contains(list, `"source":"project"`) {
		t.Fatalf("POST /api/mcp = %s", list)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil || !strings.Contains(string(data), "Bearer ${RAGFLOW_KEY}") {
		t.Fatalf(".mcp.json = %s, %v; want ragflow with its variable", data, err)
	}

	eventually(t, "dev1@dev restarted with the new server", func() bool {
		after, running := d.Supervisor.Output("dev1@dev")

		return running && after != before && d.Statuses.Get("dev1@dev") == runtime.StatusIdle
	})

	secret := `{"name":"gitlab","scope":"project","server":{"command":"gitlab-mcp","env":{"GITLAB_TOKEN":"glpat-123"}}}` //nolint:gosec // a fake secret, refused
	call(t, d, http.MethodPost, "/api/mcp", secret, http.StatusBadRequest)

	if data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json")); strings.Contains(string(data), "glpat") {
		t.Fatalf("a secret was written: %s", data)
	}

	call(t, d, http.MethodDelete, "/api/mcp/ragflow?scope=project", "", http.StatusNoContent)
	call(t, d, http.MethodDelete, "/api/mcp/ragflow?scope=project", "", http.StatusNotFound)

	if list := strings.TrimSpace(string(call(t, d, http.MethodGet, "/api/mcp", "", http.StatusOK))); list != "[]" {
		t.Fatalf("GET /api/mcp after removal = %s, want []", list)
	}
}

// Editing a server: the control room gets its command, arguments and URL,
// never a value written in clear, which must be replaced by a variable to
// save; the server's other keys are kept (ADR-0010).
func TestMCPServerEdited(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	dir := live.Current().ProjectDir

	mcpJSON := `{"mcpServers":{"gitlab":{"command":"gitlab-mcp","args":["--read-only"],"timeout":30000,` +
		`"env":{"GITLAB_TOKEN":"glpat-in-clear"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := live.Reload(); err != nil {
		t.Fatal(err)
	}

	d := startDaemon(t, live)

	detail := string(call(t, d, http.MethodGet, "/api/mcp/gitlab", "", http.StatusOK))
	if strings.Contains(detail, "glpat") || !strings.Contains(detail, `"--read-only"`) || !strings.Contains(detail, `"clear_values":["env.GITLAB_TOKEN"]`) {
		t.Fatalf("GET /api/mcp/gitlab = %s, want its arguments, and its value in clear emptied and listed", detail)
	}

	call(t, d, http.MethodPut, "/api/mcp/gitlab", `{"scope":"project","server":{"command":"gitlab-mcp","env":{"GITLAB_TOKEN":""}}}`, http.StatusBadRequest)
	call(t, d, http.MethodPut, "/api/mcp/gitlab", `{"scope":"user","server":{"command":"gitlab-mcp"}}`, http.StatusNotFound)
	call(t, d, http.MethodPut, "/api/mcp/gitlab",
		`{"scope":"project","server":{"command":"gitlab-mcp","args":["--read-write"],"env":{"GITLAB_TOKEN":"${GITLAB_TOKEN}"}}}`, http.StatusOK)

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"--read-write", "${GITLAB_TOKEN}", `"timeout": 30000`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf(".mcp.json misses %q after the change:\n%s", want, data)
		}
	}

	if strings.Contains(string(data), "glpat") {
		t.Fatalf("the value in clear is still there:\n%s", data)
	}
}
