package org

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
			{ID: "dev", Goal: "Build", Agents: []Agent{
				{Name: "dev1", Role: "Developer", Model: "sonnet", Tools: []string{"Read"}},
				{Name: "dev2", Role: "Developer", OnLeave: true},
			}},
			{ID: "qa"},
		},
		Channels: []Channel{{From: "dev", To: "specs"}},
	}

	if err := WriteProject(want); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	want.MCP = map[string]MCPServer{} // no mcp.yaml: an empty registry

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	for _, file := range []string{"org.yaml", ".gitignore", "teams/dev/dev1.md", "teams/specs/archi.md"} {
		if _, err := os.Stat(filepath.Join(dir, ".nobody", file)); err != nil {
			t.Errorf("missing .nobody/%s: %v", file, err)
		}
	}
}

func TestWriteProjectKeepsAnExistingGitignore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".nobody", ".gitignore"), "custom\n")

	if err := WriteProject(&Org{ProjectDir: dir}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".nobody", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "custom\n" {
		t.Fatalf(".gitignore overwritten: %q", data)
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

func TestLoadProjectWithoutConfigIsErrNoConfig(t *testing.T) {
	t.Parallel()

	if _, err := LoadProject(t.TempDir()); !errors.Is(err, ErrNoConfig) {
		t.Fatalf("error = %v, want ErrNoConfig", err)
	}
}

func TestMigrateLegacy(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "nobody.yaml")
	writeFile(t, legacy, `
project_dir: `+project+`
teams:
  - id: specs
    goal: Write specs
    agents:
      - name: archi
        role: Architect
        model: haiku
        prompt: |
          Design things.
  - id: dev
    agents:
      - name: dev1
        role: Developer
channels:
  - from: dev
    to: specs
`)

	dir, migrated, err := MigrateLegacy(legacy)
	if err != nil {
		t.Fatal(err)
	}

	if dir != project || !migrated {
		t.Fatalf("MigrateLegacy = %q, %v", dir, migrated)
	}

	o, err := LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}

	archi, ok := o.Member("archi@specs")
	if !ok || archi.Agent.Role != "Architect" || archi.Agent.Model != "haiku" || archi.Agent.Prompt != "Design things." {
		t.Fatalf("archi migrated as %+v", archi.Agent)
	}

	if !o.HasChannel("dev", "specs") || o.Teams[0].Goal != "Write specs" {
		t.Fatalf("teams or channels lost: %+v", o)
	}

	// A second run leaves the existing configuration alone.
	if _, migrated, err := MigrateLegacy(legacy); err != nil || migrated {
		t.Fatalf("second migration: migrated=%v err=%v", migrated, err)
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

func TestLoadProjectMCPRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mcp     string
		agent   string
		wantErr string
	}{
		{
			name:  "declared server",
			mcp:   "servers:\n  gitlab:\n    type: stdio\n    command: gitlab-mcp\n    env: {GITLAB_TOKEN: \"${GITLAB_TOKEN}\"}\n",
			agent: "---\nmcp: [gitlab]\n---\n",
		},
		{
			name:    "undeclared server",
			mcp:     "servers:\n  gitlab: {command: gitlab-mcp}\n",
			agent:   "---\nmcp: [ragflow]\n---\n",
			wantErr: `"ragflow" is not declared`,
		},
		{
			name:    "no registry at all",
			agent:   "---\nmcp: [gitlab]\n---\n",
			wantErr: "declared: none",
		},
		{
			name:    "reserved name",
			mcp:     "servers:\n  nobody: {command: x}\n",
			agent:   "---\n---\n",
			wantErr: "reserved",
		},
		{
			name:    "unknown top-level key",
			mcp:     "server:\n  gitlab: {command: x}\n",
			agent:   "---\n---\n",
			wantErr: "server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".nobody", "org.yaml"), "teams: [{id: dev}]\n")
			writeFile(t, filepath.Join(dir, ".nobody", "teams", "dev", "dev1.md"), tt.agent)

			if tt.mcp != "" {
				writeFile(t, filepath.Join(dir, ".nobody", "mcp.yaml"), tt.mcp)
			}

			o, err := LoadProject(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			env, _ := o.MCP["gitlab"]["env"].(map[string]any)
			if o.MCP["gitlab"]["command"] != "gitlab-mcp" || env["GITLAB_TOKEN"] != "${GITLAB_TOKEN}" {
				t.Fatalf("registry read as %v", o.MCP)
			}
		})
	}
}
