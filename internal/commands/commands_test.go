package commands

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func names(commands []Command) []string {
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		out = append(out, string(c.Scope)+":"+c.Name)
	}

	return out
}

func TestLoad(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	personal := filepath.Join(t.TempDir(), "commands")

	writeFile(t, filepath.Join(ProjectDir(project), "review.md"), "---\ndescription: Reviews.\nargument-hint: [pr]\nallowed-tools: Bash(gh pr view *)\n---\nReview $ARGUMENTS.\n")
	writeFile(t, filepath.Join(ProjectDir(project), "front", "test.md"), "Run the front tests.\n")
	writeFile(t, filepath.Join(ProjectDir(project), "notes.txt"), "not a command")
	writeFile(t, filepath.Join(ProjectDir(project), ".hidden", "x.md"), "hidden")
	writeFile(t, filepath.Join(ProjectDir(project), "broken.md"), "---\njust words\n---\n")

	writeFile(t, filepath.Join(personal, "review.md"), "---\ndescription: My review.\n---\nMine.\n")
	writeFile(t, filepath.Join(personal, "ns", "deep", "mine.md"), "---\ndescription: Deep.\n---\nDeep.\n")

	// A personal command may be a link to a file elsewhere (dotfiles).
	elsewhere := filepath.Join(t.TempDir(), "linked.md")
	writeFile(t, elsewhere, "Linked.\n")

	if err := os.Symlink(elsewhere, filepath.Join(personal, "linked.md")); err != nil {
		t.Fatal(err)
	}

	found, err := Load(project, personal)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"project:front:test", "project:review", "personal:linked", "personal:ns:deep:mine", "personal:review"}
	if got := names(found.Commands); !slices.Equal(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}

	if len(found.Ignored) != 1 || !strings.Contains(found.Ignored[0], "broken.md") {
		t.Errorf("ignored = %v, want broken.md", found.Ignored)
	}

	review, _ := found.Get(ScopeProject, "review")
	if review.Description != "Reviews." || review.ArgumentHint != "[pr]" || review.AllowedTools != "Bash(gh pr view *)" || review.Digest == "" {
		t.Errorf("project review = %+v", review)
	}

	if mine, _ := found.Get(ScopePersonal, "review"); !mine.Shadowed {
		t.Error("the personal review should be shadowed by the project's")
	}

	if got := names(found.Personal()); !slices.Equal(got, []string{"personal:linked", "personal:ns:deep:mine"}) {
		t.Errorf("personal commands agents get = %v", got)
	}

	if empty, err := Load(t.TempDir(), filepath.Join(t.TempDir(), "missing")); err != nil || len(empty.Commands) != 0 {
		t.Errorf("missing directories: %+v, %v", empty, err)
	}
}

func TestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   Scope
		command string
		want    string
		wantErr bool
	}{
		{name: "project", scope: ScopeProject, command: "review", want: "/p/.claude/commands/review.md"},
		{name: "namespaced personal", scope: ScopePersonal, command: "front:unit-test", want: "/home/commands/front/unit-test.md"},
		{name: "upper case", scope: ScopeProject, command: "Review", wantErr: true},
		{name: "empty segment", scope: ScopeProject, command: "front::test", wantErr: true},
		{name: "path tricks", scope: ScopeProject, command: "../x", wantErr: true},
		{name: "unknown scope", scope: "builtin", command: "x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Path("/p", "/home/commands", tt.scope, tt.command)
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != filepath.FromSlash(tt.want)) {
				t.Errorf("Path = %q, %v; want %q (error %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "front", "test.md")

	if err := Save(path, "Tests.", "  "); err == nil {
		t.Fatal("a command without prompt should be refused")
	}

	if err := Save(path, "", "Run the tests."); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(path); string(data) != "Run the tests.\n" {
		t.Fatalf("a command without description or other key is its prompt alone, got %q", data)
	}

	// An existing file keeps its other keys.
	writeFile(t, path, "---\ndescription: Old.\nallowed-tools: Bash(go test *)\n---\nOld.\n")

	if err := Save(path, "Runs the tests.", "New."); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if other, _ := f.OtherKeys(); f.Description != "Runs the tests." || f.Body != "New." || other != "allowed-tools: Bash(go test *)\n" {
		t.Errorf("after the save: %+v, other keys %q", f, other)
	}
}

func TestSaveThroughALink(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "dotfiles", "mine.md")
	writeFile(t, target, "Old.\n")

	link := filepath.Join(t.TempDir(), "mine.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Save(link, "Mine.", "New."); err != nil {
		t.Fatal(err)
	}

	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("the link should stay: %q, %v", got, err)
	}

	if data, _ := os.ReadFile(target); !strings.Contains(string(data), "New.") {
		t.Errorf("the link's target should be written, got %q", data)
	}
}

func TestCopyAndDelete(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "commands")
	from := filepath.Join(t.TempDir(), "mine.md")
	writeFile(t, from, "---\ndescription: Mine.\n---\nDo it.\n")

	to := filepath.Join(root, "ns", "deep", "mine.md")
	if err := Copy(from, to); err != nil {
		t.Fatal(err)
	}

	if err := Copy(from, to); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("copying over an existing command: %v, want fs.ErrExist", err)
	}

	writeFile(t, filepath.Join(root, "ns", "other.md"), "Other.\n")

	if err := Delete(root, to); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "ns", "deep")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the emptied directory should go: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "ns", "other.md")); err != nil {
		t.Errorf("a directory still holding a command should stay: %v", err)
	}

	if err := Delete(root, to); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("deleting a missing command: %v, want fs.ErrNotExist", err)
	}

	if _, err := os.Stat(root); err != nil {
		t.Errorf("the commands directory itself should stay: %v", err)
	}
}

func TestStage(t *testing.T) {
	t.Parallel()

	personal := t.TempDir()
	staging := t.TempDir()

	for _, rel := range []string{"one.md", "ns/two.md", "three.md"} {
		writeFile(t, filepath.Join(personal, rel), "Do it.\n")
	}

	command := func(rel string) Command {
		return Command{Name: nameOf(rel), Scope: ScopePersonal, Path: filepath.Join(personal, rel)}
	}

	staged := func() []string {
		var out []string

		root := StagedCommandsDir(staging)

		err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if err == nil && path != root {
				rel, _ := filepath.Rel(root, path)
				out = append(out, filepath.ToSlash(rel))
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}

		return out
	}

	if err := Stage(staging, personal, []Command{command("one.md"), command("ns/two.md")}); err != nil {
		t.Fatal(err)
	}

	if got := staged(); !slices.Equal(got, []string{"ns", "ns/two.md", "one.md"}) {
		t.Fatalf("staged = %v", got)
	}

	// A stray file goes, an emptied directory goes, a new command comes.
	writeFile(t, filepath.Join(StagedCommandsDir(staging), "stray.md"), "x")

	if err := Stage(staging, personal, []Command{command("one.md"), command("three.md")}); err != nil {
		t.Fatal(err)
	}

	if got := staged(); !slices.Equal(got, []string{"one.md", "three.md"}) {
		t.Fatalf("staged after the change = %v", got)
	}

	if target, err := os.Readlink(filepath.Join(StagedCommandsDir(staging), "three.md")); err != nil || target != filepath.Join(personal, "three.md") {
		t.Fatalf("three.md links to %q, %v", target, err)
	}

	if err := Stage(staging, personal, []Command{{Name: "x", Path: "/elsewhere/x.md"}}); err == nil {
		t.Error("a command outside the personal directory should be refused")
	}
}

func TestUncommitted(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	run := func(args ...string) {
		t.Helper()

		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = project

		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q")

	for _, rel := range []string{"clean.md", "edited.md", "ns/kept.md"} {
		writeFile(t, filepath.Join(ProjectDir(project), rel), "Do it.\n")
	}

	run("add", ".")
	run("commit", "-q", "-m", "commands")

	writeFile(t, filepath.Join(ProjectDir(project), "edited.md"), "Changed.\n")
	writeFile(t, filepath.Join(ProjectDir(project), "ns", "fresh.md"), "New.\n")

	found, err := Load(project, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := Uncommitted(context.Background(), project, found)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"edited", "ns:fresh"}; !slices.Equal(got, want) {
		t.Fatalf("uncommitted = %v, want %v", got, want)
	}
}
