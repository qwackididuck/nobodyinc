package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// An agent gets every command (ADR-0017): the personal ones linked in the
// staging directory of the personal skills, given with --add-dir; and it may
// not write the personal commands.
func TestLaunchGivesCommands(t *testing.T) {
	t.Parallel()

	personal := t.TempDir()
	staging := filepath.Join(t.TempDir(), "skills")

	for _, rel := range []string{"mine.md", filepath.Join("ns", "deep.md"), "review.md"} {
		path := filepath.Join(personal, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte("Do it.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	o := &org.Org{
		ProjectDir: "/tmp/project",
		Commands: commands.Found{Commands: []commands.Command{
			{Name: "review", Scope: commands.ScopeProject, Path: "/tmp/project/.claude/commands/review.md"},
			{Name: "mine", Scope: commands.ScopePersonal, Path: filepath.Join(personal, "mine.md")},
			{Name: "ns:deep", Scope: commands.ScopePersonal, Path: filepath.Join(personal, "ns", "deep.md")},
			{Name: "review", Scope: commands.ScopePersonal, Path: filepath.Join(personal, "review.md"), Shadowed: true},
		}},
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1"}}}},
	}

	m, _ := o.Member("dev1@dev")
	l := Launch{
		ClaudePath: "/bin/claude", HookPath: "/opt/nobody-hook", DaemonURL: "http://127.0.0.1:7420",
		WorkDir: func(string) string { return o.ProjectDir }, MCPConfigDir: t.TempDir(),
		SkillsStagingDir: staging, PersonalSkillsDir: t.TempDir(), PersonalCommandsDir: personal,
	}

	cmd, err := l.Command(context.Background(), o, m, Resume{})
	if err != nil {
		t.Fatal(err)
	}

	if got := argValue(t, cmd.Args, "--add-dir"); got != staging {
		t.Errorf("--add-dir = %q, want the staging directory %q", got, staging)
	}

	var s struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	decode(t, argValue(t, cmd.Args, "--settings"), &s)

	if rule := "Edit(/" + personal + "/**)"; !slices.Contains(s.Permissions.Deny, rule) {
		t.Errorf("deny rules lack %s: %v", rule, s.Permissions.Deny)
	}

	// Only the personal commands agents get are staged: not the shadowed one.
	for rel, want := range map[string]bool{"mine.md": true, filepath.Join("ns", "deep.md"): true, "review.md": false} {
		_, err := os.Lstat(filepath.Join(commands.StagedCommandsDir(staging), rel))
		if got := err == nil; got != want {
			t.Errorf("%s staged = %v, want %v", rel, got, want)
		}
	}
}
