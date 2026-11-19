package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// An agent gets every skill (ADR-0011): the personal ones through a staging
// directory given with --add-dir, its exclusions turned off with
// skillOverrides, its preloaded skills from its SessionStart hook; and it may
// not write the staging directory nor the personal skills.
func TestLaunchGivesSkills(t *testing.T) {
	t.Parallel()

	personal := t.TempDir()
	staging := filepath.Join(t.TempDir(), "skills")

	for _, name := range []string{"drawio", "review"} {
		dir := filepath.Join(personal, name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	o := &org.Org{
		ProjectDir: "/tmp/project",
		Skills: skills.Found{Skills: []skills.Skill{
			{Name: "review", Scope: skills.ScopeProject, Dir: "/tmp/project/.claude/skills/review"},
			{Name: "drawio", Scope: skills.ScopePersonal, Dir: filepath.Join(personal, "drawio")},
			{Name: "review", Scope: skills.ScopePersonal, Dir: filepath.Join(personal, "review"), Shadowed: true},
		}},
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
			{Name: "dev1", Definition: catalog.Definition{Skills: []string{"drawio"}, SkillsExclude: []string{"loop", "review"}}}, //nolint:modernize // the definition reads better as a whole
			{Name: "dev2"},
		}}},
	}

	start := func(t *testing.T, address string) []string {
		t.Helper()

		m, _ := o.Member(address)
		l := Launch{
			ClaudePath: "/bin/claude", HookPath: "/opt/nobody-hook", DaemonURL: "http://127.0.0.1:7420",
			WorkDir: func(string) string { return o.ProjectDir }, MCPConfigDir: t.TempDir(),
			SkillsStagingDir: staging, PersonalSkillsDir: personal,
		}

		cmd, err := l.Command(context.Background(), o, m, Resume{})
		if err != nil {
			t.Fatal(err)
		}

		return cmd.Args
	}

	type settings struct {
		Hooks          map[string]any    `json:"hooks"`
		SkillOverrides map[string]string `json:"skillOverrides"` //nolint:tagliatelle // Claude Code's format
		Permissions    struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}

	args := start(t, "dev1@dev")

	if got := argValue(t, args, "--add-dir"); got != staging {
		t.Errorf("--add-dir = %q, want the staging directory %q", got, staging)
	}

	var s settings
	decode(t, argValue(t, args, "--settings"), &s)

	if want := map[string]string{"loop": "off", "review": "off"}; !mapsEqual(s.SkillOverrides, want) {
		t.Errorf("skillOverrides = %v, want %v", s.SkillOverrides, want)
	}

	if _, ok := s.Hooks["SessionStart"]; !ok {
		t.Errorf("no SessionStart hook to hand over the preloaded skills: %v", s.Hooks)
	}

	for _, dir := range []string{staging, personal} {
		if rule := "Edit(/" + dir + "/**)"; !slices.Contains(s.Permissions.Deny, rule) {
			t.Errorf("deny rules lack %s: %v", rule, s.Permissions.Deny)
		}
	}

	// Only the personal skills agents get are staged: not the shadowed one.
	entries, err := os.ReadDir(skills.StagedSkillsDir(staging))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != "drawio" {
		t.Errorf("staged skills = %v, want only drawio", entries)
	}

	// An agent excluding nothing gets no skillOverrides.
	var plain settings
	decode(t, argValue(t, start(t, "dev2@dev"), "--settings"), &plain)

	if plain.SkillOverrides != nil {
		t.Errorf("dev2 excludes nothing, got skillOverrides %v", plain.SkillOverrides)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}
