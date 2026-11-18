package projects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func TestRegistry(t *testing.T) {
	t.Parallel()

	r := NewRegistry(t.TempDir())

	if list, err := r.List(); err != nil || len(list) != 0 {
		t.Fatalf("a new registry should be empty: %v %v", list, err)
	}

	shop, err := r.Add("/work/shop")
	if err != nil {
		t.Fatal(err)
	}

	if shop.ID != ID("/work/shop") || shop.Name() != "shop" || !shop.Missing() {
		t.Fatalf("entry = %+v (name %q, missing %v)", shop, shop.Name(), shop.Missing())
	}

	if again, err := r.Add("/work/shop"); err != nil || again != shop {
		t.Fatalf("adding twice = %+v, %v", again, err)
	}

	blog, err := r.Add("/work/blog")
	if err != nil {
		t.Fatal(err)
	}

	// Never opened: by name. Opened: most recent first.
	assertOrder(t, r, "blog", "shop")

	if err := r.Touch(shop.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	assertOrder(t, r, "shop", "blog")

	if err := r.Remove(blog.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Get(blog.ID); !errors.Is(err, ErrUnknownProject) {
		t.Fatalf("removed project still known: %v", err)
	}

	if err := r.Touch("nope", time.Now()); !errors.Is(err, ErrUnknownProject) {
		t.Fatalf("Touch(unknown) = %v", err)
	}
}

func assertOrder(t *testing.T, r *Registry, names ...string) {
	t.Helper()

	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(list))
	for _, e := range list {
		got = append(got, e.Name())
	}

	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("order = %v, want %v", got, names)
	}
}

//nolint:paralleltest // Create writes the shared Claude Code config
func TestForgetAndRemoveWorktrees(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "shop")

	if _, err := Create(context.Background(), home, project); err != nil {
		t.Fatal(err)
	}

	state, err := EnsureStateDir(home, project)
	if err != nil {
		t.Fatal(err)
	}

	// A team worktree with uncommitted work, as a feature leaves it.
	worktree := filepath.Join(state, "worktrees", "feature--dev")
	run(t, project, "git", "worktree", "add", "-b", "nobody/feature--dev", worktree)

	if err := os.WriteFile(filepath.Join(worktree, "draft.go"), []byte("package draft"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktrees(context.Background(), home, project); err != nil {
		t.Fatal(err)
	}

	if list := run(t, project, "git", "worktree", "list"); strings.Contains(list, "feature--dev") {
		t.Fatalf("worktree still registered:\n%s", list)
	}

	if branches := run(t, project, "git", "branch", "--list", "nobody/*"); !strings.Contains(branches, "nobody/feature--dev") {
		t.Fatal("the team branch is code: it must stay")
	}

	if err := Forget(home, project); err != nil {
		t.Fatal(err)
	}

	if org.HasConfig(project) {
		t.Fatal(".nobody/ still there")
	}

	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory still there: %v", err)
	}

	if _, err := os.Stat(filepath.Join(project, ".git")); err != nil {
		t.Fatal("the project itself must stay")
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}

	return string(out)
}
