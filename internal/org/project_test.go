package org

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
)

func TestWriteThenLoadProject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := &Org{
		ProjectDir: dir,
		Teams: []Team{
			{ID: "specs", Goal: "Write specs", Agents: []Agent{
				{Name: "archi", Role: "Architect", From: "archi@1", Prompt: "Design."},
			}},
			{ID: "dev", Goal: "Build", Block: []string{"qa"}, Agents: []Agent{
				{Name: "dev1", Role: "Developer", Model: "sonnet", Tools: []string{"Read"}},
				{Name: "dev2", Role: "Developer", OnLeave: true},
			}},
			{ID: "qa"},
		},
	}

	if err := WriteProject(want); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	want.MCP = map[string]mcpservers.Server{} // no .mcp.json, no user servers

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	for _, file := range []string{"org.yaml", "teams/dev/dev1.md", "teams/specs/archi.md"} {
		if _, err := os.Stat(filepath.Join(dir, ".nobody", file)); err != nil {
			t.Errorf("missing .nobody/%s: %v", file, err)
		}
	}
}

func TestLoadProjectErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{
			name:    "no configuration",
			wantErr: "no Nobody Inc configuration",
		},
		{
			name:    "unknown field in org.yaml",
			files:   map[string]string{"org.yaml": "project_dir: /tmp\nteams: []\n"},
			wantErr: "project_dir",
		},
		{
			name: "agents of an undeclared team",
			files: map[string]string{
				"org.yaml":           "teams: [{id: dev}]\n",
				"teams/ghosts/a.md":  "---\n---\n",
				"teams/dev/dev1.md":  "---\n---\n",
				"teams/dev/notes.md": "---\n---\n",
			},
			wantErr: "ghosts",
		},
		{
			name:    "block of an unknown team",
			files:   map[string]string{"org.yaml": "teams: [{id: dev, block: [ghosts]}]\n"},
			wantErr: "ghosts",
		},
		{
			name: "broken agent file",
			files: map[string]string{
				"org.yaml":          "teams: [{id: dev}]\n",
				"teams/dev/dev1.md": "no front matter",
			},
			wantErr: "dev1.md",
		},
		{
			name: "invalid agent name",
			files: map[string]string{
				"org.yaml":          "teams: [{id: dev}]\n",
				"teams/dev/Dev1.md": "---\n---\n",
			},
			wantErr: "Dev1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, ".nobody", name), content)
			}

			_, err := LoadProject(dir)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

// An org.yaml written before ADR-0009 still loads: its channels are counted,
// ignored, and dropped by the next write.
func TestLoadProjectIgnoresChannels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".nobody", "org.yaml"),
		"teams: [{id: specs}, {id: dev, block: [specs]}]\nchannels:\n  - {from: dev, to: specs}\n  - {from: specs, to: dev}\n")

	o, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	if o.IgnoredChannels != 2 || o.CanStart("dev", "specs") || !o.CanStart("specs", "dev") {
		t.Fatalf("ignored channels = %d, dev->specs %v, specs->dev %v: want 2, blocked, open",
			o.IgnoredChannels, o.CanStart("dev", "specs"), o.CanStart("specs", "dev"))
	}

	if err := WriteOrgFile(o); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".nobody", "org.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "channels") || !strings.Contains(string(data), "block:") {
		t.Fatalf("org.yaml after a write:\n%s\nwant the block list kept and no channels", data)
	}
}

// The dismissed subagents (ADR-0018) are written to org.yaml and read back;
// an organization without any writes no ignored_subagents key.
func TestIgnoredSubagentsRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".nobody", "org.yaml"), "teams: [{id: dev}]\n")

	o, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteOrgFile(o); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(filepath.Join(dir, ".nobody", "org.yaml")); strings.Contains(string(data), "ignored_subagents") {
		t.Fatalf("no dismissed subagent, yet org.yaml has the key:\n%s", data)
	}

	changed := o.WithIgnoredSubagents("changelog", "linter", "changelog")
	if len(o.IgnoredSubagents) != 0 {
		t.Fatal("WithIgnoredSubagents changed its receiver")
	}

	if err := WriteOrgFile(changed); err != nil {
		t.Fatal(err)
	}

	again, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got := again.IgnoredSubagents; len(got) != 2 || got[0] != "changelog" || got[1] != "linter" {
		t.Fatalf("ignored subagents read back = %v, want [changelog linter]", got)
	}
}

func TestLoadProjectWithoutConfigIsErrNoConfig(t *testing.T) {
	t.Parallel()

	if _, err := LoadProject(t.TempDir()); !errors.Is(err, ErrNoConfig) {
		t.Fatalf("error = %v, want ErrNoConfig", err)
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

// The MCP servers come from Claude Code's scopes (ADR-0010): the project's
// .mcp.json here, its user scopes being covered by internal/mcpservers.
func TestLoadProjectMCPServers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".nobody", "org.yaml"), "teams: [{id: dev}]\n")
	writeFile(t, filepath.Join(dir, ".mcp.json"),
		`{"mcpServers":{"gitlab":{"command":"gitlab-mcp","env":{"GITLAB_TOKEN":"${GITLAB_TOKEN}"}},"nobody":{"command":"x"}}}`)

	o, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	gitlab := o.MCP["gitlab"]
	if env, _ := gitlab.Config["env"].(map[string]any); gitlab.Scope != mcpservers.ScopeProject || env["GITLAB_TOKEN"] != "${GITLAB_TOKEN}" {
		t.Fatalf("servers read as %v", o.MCP)
	}

	if _, ok := o.MCP["nobody"]; ok || len(o.IgnoredMCP) != 1 {
		t.Fatalf("the office server's name should be left out with a warning: %v, ignored %v", o.MCP, o.IgnoredMCP)
	}

	writeFile(t, filepath.Join(dir, ".mcp.json"), "{broken")

	if _, err := LoadProject(dir); err == nil || !strings.Contains(err.Error(), ".mcp.json") {
		t.Fatalf("a broken .mcp.json should stop the load: %v", err)
	}
}
