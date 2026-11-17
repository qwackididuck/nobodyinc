package mcpservers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string // .mcp.json; "" for none
		claude      string // ~/.claude.json, with PROJECT for the project's path; "" for none
		wantScopes  map[string]Scope
		wantIgnored []string
		wantErr     bool
	}{
		{name: "no file at all", wantScopes: map[string]Scope{}},
		{
			name:       "the three scopes",
			project:    `{"mcpServers":{"gitlab":{"command":"gitlab-mcp"}}}`,
			claude:     `{"mcpServers":{"ragflow":{"type":"http","url":"http://r"}},"projects":{"PROJECT":{"mcpServers":{"jira":{"command":"jira-mcp"}}}}}`,
			wantScopes: map[string]Scope{"gitlab": ScopeProject, "jira": ScopeLocal, "ragflow": ScopeUser},
		},
		{
			name:       "the project wins, then local, then user",
			project:    `{"mcpServers":{"gitlab":{"command":"project"}}}`,
			claude:     `{"mcpServers":{"gitlab":{"command":"user"},"jira":{"command":"user"}},"projects":{"PROJECT":{"mcpServers":{"gitlab":{"command":"local"},"jira":{"command":"local"}}}}}`,
			wantScopes: map[string]Scope{"gitlab": ScopeProject, "jira": ScopeLocal},
			wantIgnored: []string{
				"gitlab (local): replaced by the project server of the same name",
				"gitlab (user): replaced by the project server of the same name",
				"jira (user): replaced by the local server of the same name",
			},
		},
		{
			name:       "local servers of another project stay out",
			claude:     `{"projects":{"/somewhere/else":{"mcpServers":{"jira":{"command":"jira-mcp"}}}}}`,
			wantScopes: map[string]Scope{},
		},
		{
			name:        "the office server's name is ignored",
			project:     `{"mcpServers":{"nobody":{"command":"x"}}}`,
			wantScopes:  map[string]Scope{},
			wantIgnored: []string{"nobody (project): the name is reserved for the office server"},
		},
		{name: "a broken .mcp.json is an error", project: `{"mcpServers":`, wantErr: true},
		{name: "a broken .claude.json is an error", claude: `not json`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project := t.TempDir()
			claude := filepath.Join(t.TempDir(), ".claude.json")

			if tt.project != "" {
				writeFile(t, ProjectFile(project), tt.project)
			}

			if tt.claude != "" {
				writeFile(t, claude, strings.ReplaceAll(tt.claude, "PROJECT", project))
			}

			found, err := Load(project, claude)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			scopes := map[string]Scope{}
			for name, s := range found.Servers {
				scopes[name] = s.Scope
			}

			if !reflect.DeepEqual(scopes, tt.wantScopes) || !slices.Equal(found.Ignored, tt.wantIgnored) {
				t.Fatalf("servers = %v, ignored %v\nwant %v, ignored %v", scopes, found.Ignored, tt.wantScopes, tt.wantIgnored)
			}
		})
	}
}

// Claude Code stores the local scope under the project's real path: on macOS,
// /var/... is /private/var/....
func TestLoadFindsTheLocalScopeUnderTheResolvedPath(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}

	claude := filepath.Join(t.TempDir(), ".claude.json")
	writeFile(t, claude, `{"projects":{"`+resolved+`":{"mcpServers":{"jira":{"command":"jira-mcp"}}}}}`)

	found, err := Load(link, claude)
	if err != nil {
		t.Fatal(err)
	}

	if found.Servers["jira"].Scope != ScopeLocal {
		t.Fatalf("servers = %v, want jira from the local scope", found.Servers)
	}
}

func TestClaudeConfigPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/opt/claude")

	if got, err := ClaudeConfigPath(); err != nil || got != "/opt/claude/.claude.json" {
		t.Fatalf("with CLAUDE_CONFIG_DIR: %q, %v", got, err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/home/me")

	if got, err := ClaudeConfigPath(); err != nil || got != "/home/me/.claude.json" {
		t.Fatalf("without CLAUDE_CONFIG_DIR: %q, %v", got, err)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	env := map[string]string{"TOKEN": "t0k", "EMPTY": ""}
	lookup := func(name string) (string, bool) {
		v, ok := env[name]

		return v, ok
	}

	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "plain", want: "plain"},
		{in: "Bearer ${TOKEN}", want: "Bearer t0k"},
		{in: "${MISSING:-https://gitlab.com}", want: "https://gitlab.com"},
		{in: "${EMPTY:-default}", want: "default"},
		{in: "${EMPTY}", want: ""},
		{in: "${MISSING}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := Expand(tt.in, lookup)
			if tt.wantErr {
				if !errors.Is(err, ErrMissingVariable) {
					t.Fatalf("error = %v, want ErrMissingVariable", err)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("Expand(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestReferences(t *testing.T) {
	t.Parallel()

	cfg := Config{"command": "x", "args": []any{"${B}"}, "env": map[string]any{"T": "${A}", "U": "${A:-z}"}, "headers": map[string]any{"H": "Bearer ${C}"}}

	if got := References(cfg); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("References = %v, want [A B C]", got)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		server  string
		cfg     Config
		wantErr error
	}{
		{"stdio with a variable", "gitlab", Config{"command": "gitlab-mcp", "env": map[string]any{"GITLAB_TOKEN": "${GITLAB_TOKEN}"}}, nil}, //nolint:gosec // a reference, not a credential
		{"http with a bearer variable", "rag", Config{"type": "http", "url": "http://r", "headers": map[string]any{"Authorization": "Bearer ${KEY}"}}, nil},
		{"a secret in env", "gitlab", Config{"command": "gitlab-mcp", "env": map[string]any{"GITLAB_TOKEN": "glpat-123"}}, ErrSecret},
		{"a secret in headers", "rag", Config{"type": "http", "url": "http://r", "headers": map[string]any{"Authorization": "Bearer abc"}}, ErrSecret},
		{"the office server's name", "nobody", Config{"command": "x"}, ErrInvalid},
		{"a bad name", "git lab", Config{"command": "x"}, ErrInvalid},
		{"stdio without a command", "x", Config{}, ErrInvalid},
		{"http without a url", "x", Config{"type": "http"}, ErrInvalid},
		{"an unknown type", "x", Config{"type": "ws", "url": "ws://x"}, ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := Validate(tt.server, tt.cfg); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// ListTools reaches an http server with its headers expanded, and lists its
// tools; a missing variable stops it before anything is sent.
func TestListTools(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "probe"}, nil)
	for _, name := range []string{"search", "delete_document"} {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "does " + name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0k" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{"type": "http", "url": srv.URL, "headers": map[string]any{"Authorization": "Bearer ${TOKEN}"}}

	tools, err := ListTools(context.Background(), cfg, func(name string) (string, bool) { return "t0k", name == "TOKEN" })
	if err != nil {
		t.Fatal(err)
	}

	if len(tools) != 2 || tools[0].Name != "delete_document" || tools[1].Name != "search" {
		t.Fatalf("tools = %+v, want delete_document then search", tools)
	}

	if _, err := ListTools(context.Background(), cfg, func(string) (string, bool) { return "", false }); !errors.Is(err, ErrMissingVariable) {
		t.Fatalf("without TOKEN: %v, want ErrMissingVariable", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()

	cfg := Config{
		"command": "gitlab-mcp", "args": []any{"--token", "abc"},
		"env":     map[string]any{"GITLAB_TOKEN": "${GITLAB_TOKEN}", "GITLAB_URL": "https://gitlab.example"}, //nolint:gosec // a reference and a URL
		"headers": map[string]any{"Authorization": "Bearer abc"},
	}

	got, cleared := Redact(cfg)

	env, _ := got["env"].(map[string]any)
	headers, _ := got["headers"].(map[string]any)

	if env["GITLAB_TOKEN"] != "${GITLAB_TOKEN}" || env["GITLAB_URL"] != "" || headers["Authorization"] != "" {
		t.Fatalf("redacted = %v, want references kept and values in clear emptied", got)
	}

	if !slices.Equal(cleared, []string{"env.GITLAB_URL", "headers.Authorization"}) {
		t.Fatalf("cleared = %v", cleared)
	}

	if args, _ := got["args"].([]any); len(args) != 2 {
		t.Fatalf("args = %v, want them kept: the control room hides them", got["args"])
	}

	if original, _ := cfg["env"].(map[string]any); original["GITLAB_URL"] != "https://gitlab.example" {
		t.Fatal("Redact changed its argument")
	}
}

func TestReplace(t *testing.T) {
	t.Parallel()

	current := Config{"command": "old", "args": []any{"x"}, "env": map[string]any{"A": "${A}"}, "timeout": 30000.0}
	got := Replace(current, Config{"type": "http", "url": "http://new"})

	want := Config{"type": "http", "url": "http://new", "timeout": 30000.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Replace = %v, want %v (transport replaced, timeout kept)", got, want)
	}
}
