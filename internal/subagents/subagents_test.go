package subagents

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
	"github.com/qwackididuck/nobodyinc/internal/org"
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

func convert(t *testing.T, header, body string) Subagent {
	t.Helper()

	f, err := frontmatter.Parse([]byte("---\n" + header + "\n---\n" + body))
	if err != nil {
		t.Fatal(err)
	}

	return Convert(f, ".claude/agents/x.md")
}

func TestConvert(t *testing.T) {
	t.Parallel()

	s := convert(t, `name: Code_Reviewer
description: Reviews diffs for bugs. Use it after every change.
tools: Read, Grep, Bash
disallowedTools:
  - Write
  - Bash(git push *)
model: sonnet
permissionMode: acceptEdits
skills: [review-checklist, Bad_Skill]
mcpServers: [github]
maxTurns: 20
color: blue`, "You review code.\n")

	want := catalog.Definition{
		Role:        "Reviews diffs for bugs",
		Expertise:   "Reviews diffs for bugs. Use it after every change.",
		Model:       "sonnet",
		Permissions: catalog.Permissions{Profile: catalog.ProfileAllowlist, Deny: []string{"Write", "Bash(git push *)"}},
		Skills:      []string{"review-checklist"},
		From:        "subagent:Code_Reviewer",
		Prompt:      "You review code.",
	}

	if s.Err != "" || s.Name != "Code_Reviewer" || s.AgentName != "code-reviewer" {
		t.Fatalf("subagent = %+v", s)
	}

	if s.Agent.Role != want.Role || s.Agent.Expertise != want.Expertise || s.Agent.Model != want.Model ||
		s.Agent.Permissions.Profile != want.Permissions.Profile || !slices.Equal(s.Agent.Permissions.Deny, want.Permissions.Deny) ||
		!slices.Equal(s.Agent.Skills, want.Skills) || s.Agent.From != want.From || s.Agent.Prompt != want.Prompt {
		t.Errorf("agent = %+v\nwant  %+v", s.Agent, want)
	}

	if len(s.Agent.Tools) != 0 || s.DroppedTools != "Read, Grep, Bash" {
		t.Errorf("tools must not be carried over: agent tools %v, dropped %q", s.Agent.Tools, s.DroppedTools)
	}

	if want := []string{"mcpServers", "maxTurns", "color", "skills: Bad_Skill"}; !slices.Equal(s.IgnoredKeys, want) {
		t.Errorf("ignored keys = %v, want %v", s.IgnoredKeys, want)
	}

	if err := s.Agent.Validate(); err != nil {
		t.Errorf("the converted definition should be valid: %v", err)
	}
}

func TestConvertWithoutName(t *testing.T) {
	t.Parallel()

	if s := convert(t, "description: Nameless.", "Body."); s.Err == "" {
		t.Errorf("a subagent without name should not be importable: %+v", s)
	}
}

func TestProfile(t *testing.T) {
	t.Parallel()

	tests := []struct{ mode, model, want string }{
		{"acceptEdits", "sonnet", catalog.ProfileAllowlist},
		{"auto", "opus", catalog.ProfileAuto},
		{"auto", "haiku", catalog.ProfileDefault},
		{"auto", "claude-haiku-5-5", catalog.ProfileDefault},
		{"bypassPermissions", "", catalog.ProfileDefault},
		{"plan", "", catalog.ProfileDefault},
		{"", "", catalog.ProfileDefault},
	}

	for _, tt := range tests {
		if got := Profile(tt.mode, tt.model); got != tt.want {
			t.Errorf("Profile(%q, %q) = %q, want %q", tt.mode, tt.model, got, tt.want)
		}
	}
}

func TestModel(t *testing.T) {
	t.Parallel()

	for header, want := range map[string]string{"model: inherit": "", "model: claude-opus-5-5": "claude-opus-5-5", "color: red": ""} {
		if got := convert(t, "name: x\n"+header, "B.").Agent.Model; got != want {
			t.Errorf("%s: model %q, want %q", header, got, want)
		}
	}
}

func TestAgentName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"code-reviewer":   "code-reviewer",
		"API Designer":    "api-designer",
		"db_migrator.v2":  "db-migrator-v2",
		"--weird__name--": "weird-name",
		"2fast":           "",
		"é":               "",
	} {
		if got := AgentName(name); got != want {
			t.Errorf("AgentName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRoleAndExpertiseAreShortened(t *testing.T) {
	t.Parallel()

	long := "Designs every public API of the platform with care for versioning, pagination, errors and naming conventions across all teams and services"
	s := convert(t, "name: api\ndescription: "+long, "B.")

	if r := []rune(s.Agent.Role); len(r) > roleMax || r[len(r)-1] != '…' {
		t.Errorf("role = %q (%d runes), want at most %d ending with …", s.Agent.Role, len(r), roleMax)
	}

	if s.Agent.Expertise != long {
		t.Errorf("a description under %d runes is the whole expertise, got %q", expertiseMax, s.Agent.Expertise)
	}
}

func TestLoadAndStatus(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeFile(t, filepath.Join(Dir(project), "reviewer.md"), "---\nname: reviewer\ndescription: Reviews.\n---\nReview.\n")
	writeFile(t, filepath.Join(Dir(project), "team", "linter.md"), "---\nname: linter\ndescription: Lints.\n---\nLint.\n")
	writeFile(t, filepath.Join(Dir(project), "changelog.md"), "---\nname: changelog\ndescription: Logs.\n---\nLog.\n")
	writeFile(t, filepath.Join(Dir(project), "broken.md"), "---\njust words\n---\n")
	writeFile(t, filepath.Join(Dir(project), ".hidden.md"), "---\nname: hidden\n---\n")
	writeFile(t, filepath.Join(Dir(project), "notes.txt"), "not a subagent")

	all, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, s := range all {
		names = append(names, s.Name+"@"+s.File)
	}

	want := []string{"@.claude/agents/broken.md", "changelog@.claude/agents/changelog.md", "linter@.claude/agents/team/linter.md", "reviewer@.claude/agents/reviewer.md"}
	if !slices.Equal(names, want) {
		t.Fatalf("loaded = %v, want %v", names, want)
	}

	if all[0].Err == "" {
		t.Error("broken.md should carry its error")
	}

	o := &org.Org{
		ProjectDir:       project,
		IgnoredSubagents: []string{"changelog"},
		Teams: []org.Team{{ID: "qa", Agents: []org.Agent{
			{Name: "rev", From: Origin("reviewer")},
		}}},
	}

	for name, want := range map[string]Status{"reviewer": StatusImported, "changelog": StatusIgnored, "linter": StatusPending} {
		if got, _ := StatusIn(o, name); got != want {
			t.Errorf("status of %s = %s, want %s", name, got, want)
		}
	}

	if _, address := StatusIn(o, "reviewer"); address != "rev@qa" {
		t.Errorf("reviewer imported as %q, want rev@qa", address)
	}

	if pending := Pending(o, all); len(pending) != 1 || pending[0].Name != "linter" {
		t.Errorf("pending = %+v, want linter only", pending)
	}

	if none, err := Load(t.TempDir()); err != nil || none != nil {
		t.Errorf("no .claude/agents: %v, %v", none, err)
	}
}
