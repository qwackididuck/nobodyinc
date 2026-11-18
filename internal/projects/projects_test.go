package projects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// TestMain points CLAUDE_CONFIG_DIR to a throwaway Claude Code config, so
// that trusting a project never writes the user's own ~/.claude.json.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nobody-claude-config-*")
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte("{}"), 0o600); err != nil {
		panic(err)
	}

	if err := os.Setenv("CLAUDE_CONFIG_DIR", dir); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(dir) // a leftover temporary directory is harmless

	os.Exit(code)
}

func TestStateDir(t *testing.T) {
	t.Parallel()

	a := StateDir("/home/me/.nobody", "/work/shop")
	b := StateDir("/home/me/.nobody", "/other/shop")

	if a == b {
		t.Fatal("two projects with the same name share a state directory")
	}

	if !strings.HasPrefix(filepath.Base(a), "shop-") || filepath.Dir(a) != "/home/me/.nobody/projects" {
		t.Fatalf("state dir = %s", a)
	}

	if a != StateDir("/home/me/.nobody", "/work/shop") || filepath.Base(a) != ID("/work/shop") {
		t.Fatal("state dir is not stable, or not named after the id")
	}
}

// The tests that set a project up are not parallel: they all trust a folder
// in the same Claude Code config, and concurrent writes would lose some.

//nolint:paralleltest // writes the shared Claude Code config
func TestSetUpNewFolder(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()

	got, err := SetUp(home, project)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Created || got.Git || !got.Trusted || got.Dir != project {
		t.Fatalf("SetUp = %+v, want created, not git, trusted", got)
	}

	o, err := org.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}

	if god, ok := o.Member("god"); !ok || !o.IsGod("god") || god.Agent.Prompt == "" || god.Agent.Model != "opus" {
		t.Fatalf("director not created from the built-in director: %+v", god)
	}

	if len(o.Teams) != 0 || len(o.Members()) != 1 {
		t.Fatalf("a new project should hold the director alone, got teams %+v", o.Teams)
	}

	if _, err := os.Stat(filepath.Join(home, "catalog")); err != nil {
		t.Fatalf("user catalog not created: %v", err)
	}

	if trusted, err := runtime.ProjectTrusted(project); err != nil || !trusted {
		t.Fatalf("project not trusted by Claude Code: %v %v", trusted, err)
	}
}

//nolint:paralleltest // writes the shared Claude Code config
func TestSetUpKeepsAnExistingProject(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()

	if _, err := SetUp(home, project); err != nil {
		t.Fatal(err)
	}

	if err := org.WriteProjectPrompt(project, "Keep me."); err != nil {
		t.Fatal(err)
	}

	got, err := SetUp(home, project)
	if err != nil {
		t.Fatal(err)
	}

	o, err := org.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}

	if got.Created || o.ProjectPrompt != "Keep me." {
		t.Fatalf("an existing project was rewritten: %+v, prompt %q", got, o.ProjectPrompt)
	}
}

func TestSetUpRefusesWhatIsNotAFolder(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{file, filepath.Join(t.TempDir(), "missing")} {
		if _, err := SetUp(t.TempDir(), path); !errors.Is(err, ErrNotDirectory) {
			t.Errorf("SetUp(%s) error = %v, want ErrNotDirectory", path, err)
		}
	}
}

//nolint:paralleltest // writes the shared Claude Code config
func TestCreate(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "shop")

	got, err := Create(context.Background(), home, project)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Created || !got.Git {
		t.Fatalf("Create = %+v, want a new git project", got)
	}

	// Features branch off HEAD: the new repository must have a commit.
	out, err := exec.CommandContext(t.Context(), "git", "-C", project, "log", "--format=%an %s").Output()
	if err != nil || strings.TrimSpace(string(out)) != "Nobody Inc Initial commit" {
		t.Fatalf("first commit = %q, %v", out, err)
	}

	if _, err := Create(context.Background(), home, project); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create error = %v, want ErrExists", err)
	}
}

func TestLock(t *testing.T) {
	t.Parallel()

	state := t.TempDir()

	unlock, err := Lock(state)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Lock(state); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock error = %v, want ErrLocked", err)
	}

	unlock()

	again, err := Lock(state)
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}

	again()
}
