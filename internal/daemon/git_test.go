package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/work"
	"github.com/qwackididuck/nobodyinc/internal/workspace"
)

// A delivery that changes the project's .nobody/ is refused in block mode,
// and goes through with a warning to the human in warn mode (ADR-0004).
func TestDeliveryChangingTheConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		protection  string
		wantRefused bool
	}{
		{protection: "", wantRefused: true}, // no setting: block
		{protection: settings.ProtectBlock, wantRefused: true},
		{protection: settings.ProtectWarn, wantRefused: false},
	}

	for _, tt := range tests {
		t.Run("protection="+tt.protection, func(t *testing.T) {
			t.Parallel()

			var warned []string

			g := &gitWorkspace{
				repo:              workspace.Repo{Dir: newGitProject(t), WorktreesDir: t.TempDir()},
				org:               &org.Org{Teams: []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1"}}}}},
				configProtection:  tt.protection,
				onConfigDelivered: func(_ string, _ work.Feature, files []string) { warned = files },
			}
			ctx := context.Background()

			f, err := g.PrepareFeature(ctx, work.Feature{Title: "Add orders"})
			if err != nil {
				t.Fatal(err)
			}

			dev := g.repo.TeamDir(toWorkspace(f), "dev")
			gitCommit(t, dev, ".nobody/teams/dev/dev1.md", "---\npermissions: {profile: auto}\n---\n")

			delivered, err := g.BeforeDone(ctx, f, "dev1@dev")

			if tt.wantRefused {
				if !errors.Is(err, workspace.ErrConfigChanged) || !strings.Contains(err.Error(), ".nobody/teams/dev/dev1.md") {
					t.Fatalf("err = %v, want ErrConfigChanged naming the file", err)
				}

				if len(warned) != 0 {
					t.Fatal("a refused delivery must not warn as delivered")
				}

				return
			}

			if err != nil || len(delivered) != 1 {
				t.Fatalf("warn mode should deliver: delivered=%v err=%v", delivered, err)
			}

			if len(warned) != 1 || warned[0] != ".nobody/teams/dev/dev1.md" {
				t.Fatalf("human warned about %v, want the changed file", warned)
			}
		})
	}
}

var gitIdentity = []string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}

// newGitProject is a repository with one commit on main.
func newGitProject(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	gitCommit(t, dir, "README.md", "hello\n")

	return dir
}

func gitCommit(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "add", name)
	runGit(t, dir, append(gitIdentity, "commit", "-m", "chore: "+name)...)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
