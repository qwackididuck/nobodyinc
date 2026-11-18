//go:build contract

package contract

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// How skills reach an agent, and how Nobody Inc can shape what it gets,
// with the flags agents are started with (agentSources, --strict-mcp-config).
// Checked on 2026-11-18 with Claude Code 2.1.327 (roadmap, "Skills des
// agents"):
//   - the user's personal skills are not loaded without the "user" settings
//     source, but a directory given with --add-dir brings its own
//     .claude/skills, symbolic links included;
//   - a skill whose header is not valid YAML is still loaded;
//   - skillOverrides {"<name>": "off"} hides a skill, built-in or not, from
//     the model; a deny rule would leave it listed;
//   - a session's init event lists every skill even when the API cannot be
//     reached, which is how the daemon discovers the built-in ones for free;
//   - the skills: field of an agent started with --agent preloads nothing on
//     the main thread, while a SessionStart hook can hand over content to a
//     resumed session, which keeps its memory;
//   - invoking a skill that declares hooks asks for permission; once allowed,
//     its hooks run.

// unreachableAPI is an API address nothing listens on: a session started
// with it emits its init event, then fails to reach the model.
const unreachableAPI = "ANTHROPIC_BASE_URL=http://127.0.0.1:1"

// The user's personal skills (<config dir>/skills) do not reach an agent;
// a directory given with --add-dir brings its .claude/skills, and a symbolic
// link to a skill directory in it is followed. No model is called.
func TestPersonalSkillsReachAgentsThroughAnAddedDirectory(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	configDir := t.TempDir()
	writeSkill(t, filepath.Join(configDir, "skills"), "personal-probe", "Say PROBE.")

	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, ".claude", "skills"), 0o750); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(staging, ".claude", "skills", "personal-probe")
	if err := os.Symlink(filepath.Join(configDir, "skills", "personal-probe"), link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		addDir  bool
		wantSee bool
	}{
		{name: "agent flags alone: not loaded"},
		{name: "with --add-dir on a staging directory: loaded", addDir: true, wantSee: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var extra []string
			if tt.addDir {
				extra = []string{"--add-dir", staging}
			}

			skills := initSkills(t, project, []string{"CLAUDE_CONFIG_DIR=" + configDir}, extra...)

			if got := slices.Contains(skills, "personal-probe"); got != tt.wantSee {
				t.Errorf("personal skill loaded = %v, want %v: %v", got, tt.wantSee, skills)
			}
		})
	}
}

// skillOverrides {"<name>": "off"} in --settings takes a skill away from the
// model, a project skill as well as a built-in one: how an agent's
// skills_exclude is applied. No model is called.
func TestSkillOverridesHideSkills(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeSkill(t, filepath.Join(project, ".claude", "skills"), "kept-probe", "Say KEPT.")
	writeSkill(t, filepath.Join(project, ".claude", "skills"), "hidden-probe", "Say HIDDEN.")

	before := initSkills(t, project, nil)
	if !slices.Contains(before, "loop") {
		t.Fatalf("the built-in skill loop is gone, pick another one for this test: %v", before)
	}

	after := initSkills(t, project, nil, "--settings", `{"skillOverrides":{"hidden-probe":"off","loop":"off"}}`)

	for _, name := range []string{"hidden-probe", "loop"} {
		if slices.Contains(after, name) {
			t.Errorf("%s is still listed after skillOverrides off: %v", name, after)
		}
	}

	if !slices.Contains(after, "kept-probe") {
		t.Errorf("kept-probe should stay listed: %v", after)
	}
}

// A skill whose header is not valid YAML, such as an unquoted description
// holding ": ", is still loaded: skills.ParseFile reads such a header line by
// line instead of refusing it. No model is called.
func TestSkillWithAnInvalidYAMLHeaderIsLoaded(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeProjectFile(t, project, ".claude/skills/colon-probe/SKILL.md",
		"---\nname: colon-probe\ndescription: Reviews a change: tests, naming, docs.\n---\nSay PROBE.\n")

	if skills := initSkills(t, project, nil); !slices.Contains(skills, "colon-probe") {
		t.Errorf("colon-probe is not loaded: %v", skills)
	}
}

// The init event lists the built-in skills with the API out of reach, so
// the daemon can discover them without calling the model. Killing a normal
// session right after its init event would not do: the first API request
// leaves slightly before the init event.
func TestInitListsSkillsWithoutTheAPI(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeSkill(t, filepath.Join(project, ".claude", "skills"), "project-probe", "Say PROBE.")

	skills := initSkills(t, project, nil)

	for _, name := range []string{"project-probe", "loop"} {
		if !slices.Contains(skills, name) {
			t.Errorf("init without the API does not list %s: %v", name, skills)
		}
	}
}

// The skills: field of a subagent definition preloads nothing when that
// agent runs the main thread (--agents and --agent), although its prompt is
// applied. That is why Nobody Inc hands preloaded skills over itself. If
// Claude Code ever preloads them, the test says so.
func TestAgentSkillsFieldIsNotPreloadedOnTheMainThread(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeSkill(t, filepath.Join(project, ".claude", "skills"), "codeword", "The secret codeword is TOUCAN.")

	agents := `{"probe":{"description":"A probe agent.","prompt":"The fruit codeword is MANGO.","skills":["codeword"]}}`
	out := askProject(t, project,
		"Without using any tool, give the fruit and secret codewords you were given, as fruit=<word or NONE> secret=<word or NONE>.",
		"--agents", agents, "--agent", "probe", "--disallowedTools", "Skill")

	answer := strings.ToUpper(streamResult(t, out))
	if !strings.Contains(answer, "MANGO") {
		t.Fatalf("the agent's prompt should apply on the main thread, got %q", answer)
	}

	if strings.Contains(answer, "TOUCAN") {
		t.Logf("Claude Code now preloads skills: on the main thread (answer %q): "+
			"Nobody Inc could hand preloading over to --agent", answer)
	}
}

// A SessionStart hook hands content over to a resumed session through
// additionalContext, and the session keeps its memory: how preloaded skills
// reach an agent without losing its conversation.
func TestSessionStartHandsContentToAResumedSession(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	hook := sessionStartScript(t, project, "The secret codeword is TOUCAN.")

	withModelRetry(t, func() string {
		first := streamSessionID(t, askProject(t, project, "Remember: the fruit codeword is MANGO. Answer only OK."))

		out := askProject(t, project,
			"Without using any tool, give the fruit and secret codewords, as fruit=<word or NONE> secret=<word or NONE>.",
			"--resume", first, "--settings", commandHookSettings(t, "SessionStart", hook))

		answer := strings.ToUpper(streamResult(t, out))
		if !strings.Contains(answer, "MANGO") || !strings.Contains(answer, "TOUCAN") {
			return "want both MANGO (memory) and TOUCAN (hook), got " + answer
		}

		return ""
	})
}

// Invoking a skill that declares hooks asks for permission (a skill without
// hooks does not); once the skill is allowed, its hooks run. For an agent,
// such an invocation goes to the human's approval queue.
func TestSkillWithHooksAsksPermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		allow     bool
		wantFired bool
	}{
		{name: "not allowed: denied, hook silent"},
		{name: "allowed: its hook runs", allow: true, wantFired: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project := t.TempDir()
			marker := filepath.Join(project, "hook-fired")
			body := "---\nname: hooked-probe\ndescription: A probe skill with a hook.\nhooks:\n" +
				"  PreToolUse:\n    - matcher: \"Bash\"\n      hooks:\n        - type: command\n" +
				"          command: \"touch '" + marker + "'\"\n---\nRun the bash command: echo hello. Then reply DONE.\n"
			writeProjectFile(t, project, ".claude/skills/hooked-probe/SKILL.md", body)

			allowed := "Bash(echo:*)"
			if tt.allow {
				allowed += ",Skill(hooked-probe)"
			}

			withModelRetry(t, func() string {
				out := askProject(t, project, "Invoke the hooked-probe skill with the Skill tool and follow it.",
					"--allowedTools", allowed)

				if !invokedSkill(out, "hooked-probe") {
					return "the model did not invoke the skill"
				}

				if denied := deniedTools(out); slices.Contains(denied, "Skill") == tt.allow {
					t.Fatalf("Skill denied = %v, want %v", !tt.allow, tt.allow)
				}

				if _, err := os.Stat(marker); (err == nil) != tt.wantFired {
					t.Fatalf("skill hook fired = %v, want %v", err == nil, tt.wantFired)
				}

				return ""
			})
		})
	}
}

// initSkills starts a session in dir with the agents' flags and the API out
// of reach, and returns the skills its init event lists. The session is
// killed as soon as the init event arrives.
func initSkills(t *testing.T, dir string, env []string, extraArgs ...string) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	args := append([]string{
		"-p", "Answer OK.", "--model", "haiku", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
	}, agentSources...)

	cmd := exec.CommandContext(ctx, "claude", append(args, extraArgs...)...) //nolint:gosec // test arguments
	cmd.Dir = dir

	cmd.Env = append(append(os.Environ(), env...), unreachableAPI)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = cmd.Process.Kill() // the session would retry the API until the timeout
		_ = cmd.Wait()         // killed on purpose: its exit status says nothing
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var ev projectInitEvent
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			return ev.Skills
		}
	}

	t.Fatalf("no init event before the session ended: %v", scanner.Err())

	return nil
}

// writeSkill writes <dir>/<name>/SKILL.md with the given body.
func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()

	writeProjectFile(t, dir, filepath.Join(name, "SKILL.md"),
		"---\nname: "+name+"\ndescription: A probe skill.\n---\n"+body+"\n")
}

// sessionStartScript writes a SessionStart hook that hands context over to
// the session, and returns its path.
func sessionStartScript(t *testing.T, dir, additionalContext string) string {
	t.Helper()

	output, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName": "SessionStart", "additionalContext": additionalContext,
	}})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "session-start.sh")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + string(output) + "'\n"

	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}

	return path
}

// commandHookSettings is a --settings value wiring one hook event to a
// command.
func commandHookSettings(t *testing.T, event, command string) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"hooks": map[string]any{
		event: []map[string]any{{"hooks": []map[string]string{{"type": "command", "command": command}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// streamSessionID returns the session ID of a stream-json session.
func streamSessionID(t *testing.T, out []byte) string {
	t.Helper()

	var id string

	eachEvent(out, func(line []byte) bool {
		var ev struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.SessionID != "" {
			id = ev.SessionID

			return false
		}

		return true
	})

	if id == "" {
		t.Fatalf("no session ID in claude output:\n%s", out)
	}

	return id
}

// invokedSkill reports whether the session called the Skill tool on name.
func invokedSkill(out []byte, name string) bool {
	found := false

	eachEvent(out, func(line []byte) bool {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Skill string `json:"skill"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "assistant" {
			return true
		}

		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" && c.Name == "Skill" && c.Input.Skill == name {
				found = true

				return false
			}
		}

		return true
	})

	return found
}

// deniedTools returns the tools the session was refused, from its result
// event.
func deniedTools(out []byte) []string {
	var denied []string

	eachEvent(out, func(line []byte) bool {
		var ev struct {
			Type              string `json:"type"`
			PermissionDenials []struct {
				ToolName string `json:"tool_name"`
			} `json:"permission_denials"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "result" {
			return true
		}

		for _, d := range ev.PermissionDenials {
			denied = append(denied, d.ToolName)
		}

		return false
	})

	return denied
}
