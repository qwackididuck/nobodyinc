package skills

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

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // test paths
		t.Fatal(err)
	}
}

func writeSkill(t *testing.T, dir, name, header string) {
	t.Helper()

	writeFile(t, filepath.Join(dir, name, skillFile), "---\nname: "+name+"\n"+header+"---\nDo it.\n")
}

func names(skills []Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, string(s.Scope)+":"+s.Name)
	}

	return out
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(t *testing.T, project, personal string)
		want        []string
		wantIgnored int
		check       func(t *testing.T, f Found)
	}{
		{
			name:  "no skills directories",
			setup: func(*testing.T, string, string) {},
		},
		{
			name: "project then personal, each by name",
			setup: func(t *testing.T, project, personal string) {
				t.Helper()
				writeSkill(t, ProjectDir(project), "zeta", "description: Z.\n")
				writeSkill(t, ProjectDir(project), "alpha", "description: A.\n")
				writeSkill(t, personal, "drawio", "description: Diagrams.\n")
			},
			want: []string{"project:alpha", "project:zeta", "personal:drawio"},
			check: func(t *testing.T, f Found) {
				t.Helper()

				if s, _ := f.Get(ScopePersonal, "drawio"); s.Description != "Diagrams." || s.Digest == "" {
					t.Errorf("drawio = %+v, want its description and a digest", s)
				}
			},
		},
		{
			name: "a personal skill named like a project skill is shadowed",
			setup: func(t *testing.T, project, personal string) {
				t.Helper()
				writeSkill(t, ProjectDir(project), "review", "description: Project.\n")
				writeSkill(t, personal, "review", "description: Mine.\n")
				writeSkill(t, personal, "solo", "description: Mine.\n")
			},
			want: []string{"project:review", "personal:review", "personal:solo"},
			check: func(t *testing.T, f Found) {
				t.Helper()

				if got := names(f.Personal()); !slices.Equal(got, []string{"personal:solo"}) {
					t.Errorf("personal skills agents get = %v, want only solo", got)
				}

				if s, _ := f.ForAgent("review"); s.Scope != ScopeProject {
					t.Errorf("agents get review from %s, want the project", s.Scope)
				}
			},
		},
		{
			name: "hooks are noticed",
			setup: func(t *testing.T, project, _ string) {
				t.Helper()
				writeSkill(t, ProjectDir(project), "hooked", "description: H.\nhooks:\n  PreToolUse: []\n")
			},
			want: []string{"project:hooked"},
			check: func(t *testing.T, f Found) {
				t.Helper()

				if s, _ := f.Get(ScopeProject, "hooked"); !s.HasHooks {
					t.Error("hooked should declare hooks")
				}
			},
		},
		{
			name: "broken header ignored, directory without SKILL.md and hidden directories skipped",
			setup: func(t *testing.T, project, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(ProjectDir(project), "broken", skillFile), "---\nnot a key, not a comment\n---\n")
				writeFile(t, filepath.Join(ProjectDir(project), "notes", "README.md"), "not a skill")
				writeSkill(t, ProjectDir(project), ".hidden", "description: H.\n")
				writeSkill(t, ProjectDir(project), "fine", "description: F.\n")
			},
			want:        []string{"project:fine"},
			wantIgnored: 1,
		},
		{
			name: "a symbolic link to a skill directory is a skill",
			setup: func(t *testing.T, _, personal string) {
				t.Helper()

				elsewhere := t.TempDir()
				writeSkill(t, elsewhere, "linked", "description: L.\n")

				if err := os.MkdirAll(personal, 0o750); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(personal, "linked")); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{"personal:linked"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project := t.TempDir()
			personal := filepath.Join(t.TempDir(), "skills")
			tt.setup(t, project, personal)

			found, err := Load(project, personal)
			if err != nil {
				t.Fatal(err)
			}

			if got := names(found.Skills); !slices.Equal(got, tt.want) {
				t.Errorf("skills = %v, want %v", got, tt.want)
			}

			if len(found.Ignored) != tt.wantIgnored {
				t.Errorf("ignored = %v, want %d", found.Ignored, tt.wantIgnored)
			}

			if tt.check != nil {
				tt.check(t, found)
			}
		})
	}
}

func TestDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   Scope
		skill   string
		want    string
		wantErr bool
	}{
		{name: "project", scope: ScopeProject, skill: "review", want: "/p/.claude/skills/review"},
		{name: "personal", scope: ScopePersonal, skill: "review", want: "/home/skills/review"},
		{name: "built-in refused", scope: ScopeBuiltin, skill: "loop", wantErr: true},
		{name: "unknown scope refused", scope: "team", skill: "review", wantErr: true},
		{name: "path in the name refused", scope: ScopeProject, skill: "../evil", wantErr: true},
		{name: "uppercase refused", scope: ScopeProject, skill: "Review", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Dir("/p", "/home/skills", tt.scope, tt.skill)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Dir = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestFileKeepsUnknownHeaderKeys(t *testing.T) {
	t.Parallel()

	original := "---\nname: old-name\ndescription: Old.\n# who may use it\nallowed-tools: Read, Grep\nhooks:\n  PreToolUse:\n    - matcher: Bash\n---\nOld body.\n"

	f, err := ParseFile([]byte(original))
	if err != nil {
		t.Fatal(err)
	}

	if f.Description != "Old." || f.Body != "Old body." || !f.HasHooks() {
		t.Fatalf("parsed %+v", f)
	}

	f.Description = "New: with a colon."
	f.Body = "New body."

	data, err := f.Format("review")
	if err != nil {
		t.Fatal(err)
	}

	got := string(data)
	for _, want := range []string{
		"name: review\n", `description: 'New: with a colon.'` + "\n", "# who may use it\nallowed-tools: Read, Grep\n",
		"hooks:\n  PreToolUse:\n    - matcher: Bash\n", "---\n\nNew body.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted file lacks %q:\n%s", want, got)
		}
	}

	again, err := ParseFile(data)
	if err != nil || again.Description != "New: with a colon." || again.Body != "New body." {
		t.Fatalf("round trip = %+v, %v", again, err)
	}

	other, err := again.OtherKeys()
	if err != nil || !strings.HasPrefix(other, "# who may use it\nallowed-tools: Read, Grep\nhooks:") {
		t.Errorf("other keys = %q, %v", other, err)
	}
}

// Claude Code loads a skill whose header is not valid YAML, such as an
// unquoted description holding ": " (checked by test/contract): so does
// Nobody Inc, and saving it writes a valid header with the other lines kept.
func TestParseFileWithAnInvalidYAMLHeader(t *testing.T) {
	t.Parallel()

	original := "---\nname: review\ndescription: Reviews a change: tests, naming, docs.\nallowed-tools: Bash(git log *)\nhooks:\n  PostToolUse: []\n---\nCheck the tests.\n"

	f, err := ParseFile([]byte(original))
	if err != nil {
		t.Fatal(err)
	}

	if f.Description != "Reviews a change: tests, naming, docs." || !f.HasHooks() || f.Body != "Check the tests." {
		t.Fatalf("parsed %+v", f)
	}

	if other, _ := f.OtherKeys(); other != "allowed-tools: Bash(git log *)\nhooks:\n  PostToolUse: []\n" {
		t.Errorf("other keys = %q", other)
	}

	data, err := f.Format("review")
	if err != nil {
		t.Fatal(err)
	}

	again, err := ParseFile(data)
	if err != nil || again.looseOther != "" || again.Description != f.Description || !again.HasHooks() {
		t.Fatalf("saved file is not valid YAML or lost something:\n%s\n%+v, %v", data, again, err)
	}

	if _, err := ParseFile([]byte("---\njust some words\n---\n")); err == nil {
		t.Error("a header line that is neither a key nor a comment should be refused")
	}
}

func TestParseFileWithoutHeader(t *testing.T) {
	t.Parallel()

	f, err := ParseFile([]byte("Just instructions.\n"))
	if err != nil || f.Body != "Just instructions." || f.Description != "" {
		t.Fatalf("ParseFile = %+v, %v", f, err)
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "review")

	if err := Save(dir, " ", "Body."); err == nil {
		t.Fatal("a skill without description should be refused")
	}

	if err := Save(dir, "Reviews code.", "First."); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "scripts", "lint.sh"), "#!/bin/sh\n")

	// An existing file keeps its other keys and files.
	data, _ := os.ReadFile(filepath.Join(dir, skillFile))
	writeFile(t, filepath.Join(dir, skillFile), strings.Replace(string(data), "---\n\n", "model: haiku\n---\n\n", 1))

	if err := Save(dir, "Reviews code carefully.", "Second."); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}

	other, _ := f.OtherKeys()
	if f.Description != "Reviews code carefully." || f.Body != "Second." || other != "model: haiku\n" {
		t.Errorf("after the second save: %+v, other keys %q", f, other)
	}

	if _, err := os.Stat(filepath.Join(dir, "scripts", "lint.sh")); err != nil {
		t.Errorf("the skill's other files should stay: %v", err)
	}
}

func TestCopyAndDelete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	from := filepath.Join(root, "personal", "review")
	to := filepath.Join(root, "project", "review")

	writeSkill(t, filepath.Join(root, "personal"), "review", "description: R.\n")
	writeFile(t, filepath.Join(from, "scripts", "check.sh"), "#!/bin/sh\n")

	if err := os.Chmod(filepath.Join(from, "scripts", "check.sh"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := Copy(from, to); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(to, "scripts", "check.sh"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("copied script: %v, %v; want it executable", info, err)
	}

	if err := Copy(from, to); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("copying over an existing skill = %v, want fs.ErrExist", err)
	}

	if err := Delete(to); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(to); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted skill still there: %v", err)
	}

	if _, err := os.Stat(from); err != nil {
		t.Fatalf("the original should stay: %v", err)
	}

	if err := Delete(to); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleting a missing skill = %v, want fs.ErrNotExist", err)
	}
}

func TestStage(t *testing.T) {
	t.Parallel()

	personal := t.TempDir()
	staging := t.TempDir()

	for _, name := range []string{"one", "two", "three"} {
		writeSkill(t, personal, name, "description: S.\n")
	}

	skill := func(name string) Skill {
		return Skill{Name: name, Scope: ScopePersonal, Dir: filepath.Join(personal, name)}
	}

	staged := func() []string {
		entries, err := os.ReadDir(StagedSkillsDir(staging))
		if err != nil {
			t.Fatal(err)
		}

		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}

		return out
	}

	if err := Stage(staging, []Skill{skill("one"), skill("two")}); err != nil {
		t.Fatal(err)
	}

	if got := staged(); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("staged = %v", got)
	}

	// A stray file and a link now pointing elsewhere are replaced.
	writeFile(t, filepath.Join(StagedSkillsDir(staging), "stray"), "x")

	if err := Stage(staging, []Skill{skill("two"), skill("three")}); err != nil {
		t.Fatal(err)
	}

	if got := staged(); !slices.Equal(got, []string{"three", "two"}) {
		t.Fatalf("staged after the change = %v", got)
	}

	if target, err := os.Readlink(filepath.Join(StagedSkillsDir(staging), "three")); err != nil || target != filepath.Join(personal, "three") {
		t.Fatalf("three links to %q, %v", target, err)
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

	for _, name := range []string{"clean", "edited", "renamed-from"} {
		writeSkill(t, ProjectDir(project), name, "description: S.\n")
	}

	run("add", ".")
	run("commit", "-q", "-m", "skills")

	writeSkill(t, ProjectDir(project), "fresh", "description: S.\n")
	writeFile(t, filepath.Join(ProjectDir(project), "edited", skillFile), "---\ndescription: Changed.\n---\n")
	run("mv", ".claude/skills/renamed-from", ".claude/skills/renamed-to")

	found, err := Load(project, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := Uncommitted(context.Background(), project, found)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"edited", "fresh", "renamed-to"}; !slices.Equal(got, want) {
		t.Fatalf("uncommitted = %v, want %v", got, want)
	}
}

func TestDiscoverBuiltin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		script  string
		want    []string
		wantErr string
	}{
		{
			name: "built-in skills are those neither in the project nor personal",
			script: `echo '{"type":"system","subtype":"hook_started"}'
echo '{"type":"system","subtype":"init","skills":["review","loop","drawio","simplify","loop"]}'
sleep 30`,
			want: []string{"loop", "simplify"},
		},
		{
			name:    "no init event",
			script:  `echo '{"type":"result"}'`,
			wantErr: "ended without listing its skills",
		},
	}

	known := Found{Skills: []Skill{{Name: "review", Scope: ScopeProject}, {Name: "drawio", Scope: ScopePersonal}}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			claude := filepath.Join(dir, "claude")
			writeFile(t, claude, "#!/bin/sh\n"+tt.script+"\n")

			if err := os.Chmod(claude, 0o700); err != nil {
				t.Fatal(err)
			}

			got, err := DiscoverBuiltin(context.Background(), Discovery{ClaudePath: claude, Dir: dir, Env: os.Environ()}, known)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}

				return
			}

			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("DiscoverBuiltin = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestBuiltinKeepsTheLastListOnFailure(t *testing.T) {
	t.Parallel()

	results := []error{nil, errors.New("claude not found")}
	b := NewBuiltin(func(context.Context) ([]string, error) {
		err := results[0]
		results = results[1:]

		return []string{"loop"}, err
	})

	if _, err := b.Names(); err == nil {
		t.Fatal("before the first refresh, Names should say so")
	}

	b.Refresh(context.Background())
	b.Refresh(context.Background())

	got, err := b.Names()
	if !slices.Equal(got, []string{"loop"}) || err == nil {
		t.Fatalf("Names = %v, %v; want the last list and the last error", got, err)
	}
}

func TestPreload(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	worktree := t.TempDir()
	personal := t.TempDir()

	writeFile(t, filepath.Join(ProjectDir(project), "review", skillFile), "---\ndescription: R.\n---\nMain checkout version.\n")
	writeFile(t, filepath.Join(ProjectDir(worktree), "review", skillFile), "---\ndescription: R.\n---\nCommitted version.\n")
	writeFile(t, filepath.Join(personal, "drawio", skillFile), "---\ndescription: D.\n---\nDraw.\n")

	found, err := Load(project, personal)
	if err != nil {
		t.Fatal(err)
	}

	preloaded, missing := Preload([]string{"review", "drawio", "ghost"}, worktree, found)

	want := []Preloaded{
		{Name: "review", Dir: filepath.Join(ProjectDir(worktree), "review"), Body: "Committed version."},
		{Name: "drawio", Dir: filepath.Join(personal, "drawio"), Body: "Draw."},
	}
	if !slices.Equal(preloaded, want) {
		t.Errorf("preloaded = %+v, want %+v", preloaded, want)
	}

	if !slices.Equal(missing, []string{"ghost"}) {
		t.Errorf("missing = %v, want [ghost]", missing)
	}
}
