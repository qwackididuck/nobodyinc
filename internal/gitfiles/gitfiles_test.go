package gitfiles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestStatusPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want []string
	}{
		{name: "nothing", out: "", want: nil},
		{name: "modified and untracked", out: " M a/x.md\x00?? a/y.md\x00", want: []string{"a/x.md", "a/y.md"}},
		{name: "rename: the original path is skipped", out: "R  a/new.md\x00a/old.md\x00 M a/z.md\x00", want: []string{"a/new.md", "a/z.md"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := statusPaths(tt.out); !slices.Equal(got, tt.want) {
				t.Errorf("statusPaths = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOf(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()

		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo

		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()

		path := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q")
	write("dir/committed.md", "a")
	write("dir/changed.md", "a")
	write("other.md", "a")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	write("dir/changed.md", "b")
	write("dir/sub/new.md", "a")

	state, err := Of(context.Background(), repo, "dir")
	if err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]bool{
		"dir/committed.md": true,
		"dir/changed.md":   false,
		"dir/sub/new.md":   false,
	} {
		if got := state.Committed(path); got != want {
			t.Errorf("Committed(%s) = %v, want %v", path, got, want)
		}
	}

	if state.Tracked["other.md"] {
		t.Error("files outside dir should not be listed")
	}

	if got := Under(state.Changed, "dir"); !slices.Contains(got, "sub/new.md") || !slices.Contains(got, "changed.md") || len(got) != 2 {
		t.Errorf("Under(changed) = %q", got)
	}
}
