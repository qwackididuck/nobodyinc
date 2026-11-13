package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustedIn(t *testing.T) {
	t.Parallel()

	// Real paths, as Claude Code records them (t.TempDir may be a symlink).
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	project := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(project, ".nobody", "worktrees", "f", "dev"), 0o750); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		projects map[string]bool
		dir      string
		want     bool
	}{
		{name: "nothing trusted", projects: nil, dir: project, want: false},
		{name: "project trusted", projects: map[string]bool{project: true}, dir: project, want: true},
		{name: "parent trusted", projects: map[string]bool{base: true}, dir: project, want: true},
		{name: "parent trusted, child entry false", projects: map[string]bool{base: true, project: false}, dir: project, want: true},
		{name: "explicitly not trusted", projects: map[string]bool{project: false}, dir: project, want: false},
		{name: "worktree covered by the project", projects: map[string]bool{project: true}, dir: filepath.Join(project, ".nobody", "worktrees", "f", "dev"), want: true},
		{name: "symlink resolved", projects: map[string]bool{project: true}, dir: link, want: true},
		{name: "a child does not cover its parent", projects: map[string]bool{filepath.Join(project, ".nobody"): true}, dir: project, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			projects := map[string]any{}
			for p, trusted := range tt.projects {
				projects[p] = map[string]bool{"hasTrustDialogAccepted": trusted}
			}

			path := filepath.Join(t.TempDir(), ".claude.json")
			data, _ := json.Marshal(map[string]any{"projects": projects}) // maps of strings and bools always encode

			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := trustedIn(path, tt.dir)
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatalf("trusted = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrustedInWithoutConfig(t *testing.T) {
	t.Parallel()

	got, err := trustedIn(filepath.Join(t.TempDir(), "missing.json"), t.TempDir())
	if err != nil || got {
		t.Fatalf("no Claude Code config: trusted=%v err=%v, want false and no error", got, err)
	}
}
