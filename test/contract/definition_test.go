//go:build contract

package contract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The permission modes used by the profiles (internal/runtime/permissions.go)
// are accepted by Claude Code and reported back in the session's init event.
//
// Auto mode is not available with Haiku: Claude Code then silently runs in
// the default mode, hence the sonnet model for it (and catalog.Validate
// refusing auto with Haiku).
func TestPermissionModesOfTheProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode, model, reported string
	}{
		{mode: "manual", model: "haiku", reported: "default"},
		{mode: "acceptEdits", model: "haiku", reported: "acceptEdits"},
		{mode: "auto", model: "sonnet", reported: "auto"},
		{mode: "auto", model: "haiku", reported: "default"}, // the fallback catalog.Validate prevents
	}

	for _, tt := range tests {
		t.Run(tt.mode+"/"+tt.model, func(t *testing.T) {
			t.Parallel()

			args := []string{
				"-p", "Answer OK.", "--model", tt.model, "--output-format", "stream-json", "--verbose",
				"--setting-sources", "local", "--permission-mode", tt.mode,
			}

			if got := initPermissionMode(t, runCLI(t, "", nil, args)); got != tt.reported {
				t.Fatalf("init event reports permission mode %q, want %q", got, tt.reported)
			}
		})
	}
}

// Under acceptEdits (the allowlist profile), file edits in the working
// directory pass without asking, but the deny rules protecting .nobody/ still
// win: an agent cannot rewrite its own configuration (ADR-0004).
func TestDenyRulesProtectTheConfigDirectory(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".nobody"), 0o750); err != nil {
		t.Fatal(err)
	}

	// Edit alone covers the Write tool: a Write rule "never matches" and Claude
	// Code warns about it at every start (runtime.protectedRules).
	settings := `{"permissions":{"deny":["Edit(./.nobody/**)"]}}`

	runClaude(t, claudeRun{
		prompt: "Use the Write tool to create two files, each containing the word x: " +
			"allowed.txt, then .nobody/hacked.txt. If one cannot be written, say so and continue.",
		workDir:   workDir,
		extraArgs: []string{"--permission-mode", "acceptEdits", "--settings", settings},
	})

	if _, err := os.Stat(filepath.Join(workDir, "allowed.txt")); err != nil {
		t.Fatalf("acceptEdits should let the agent write in its working directory: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workDir, ".nobody", "hacked.txt")); err == nil {
		t.Fatal("the deny rule did not protect .nobody/")
	}
}

// ${VARIABLE} references in --mcp-config are expanded by Claude Code from its
// environment: the MCP servers Nobody Inc writes never hold secrets, only
// references to variables set where the daemon runs (ADR-0010).
func TestMCPConfigExpandsEnvironmentVariables(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	probe := filepath.Join(dir, "probe.sh")

	// The "MCP server" only proves it was started with the expanded command.
	if err := os.WriteFile(probe, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}

	config := `{"mcpServers":{"probe":{"command":"${NOBODY_CONTRACT_PROBE}"}}}`
	args := []string{
		"-p", "Answer OK.", "--model", "haiku", "--output-format", "json",
		"--setting-sources", "local", "--strict-mcp-config", "--mcp-config", config,
	}

	runCLI(t, "", []string{"NOBODY_CONTRACT_PROBE=" + probe}, args)

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the MCP server command ${NOBODY_CONTRACT_PROBE} was not expanded and started")
	}
}

// initPermissionMode returns the permission mode of the init event of a
// stream-json session.
func initPermissionMode(t *testing.T, out []byte) string {
	t.Helper()

	return streamInit(t, out).PermissionMode
}

// A resumed session keeps its memory and follows the new model, permission
// mode and MCP servers: this is how an agent restarts after a change that
// does not touch its instructions (runtime.Supervisor.Apply).
//
// It keeps its original system prompt: Claude Code ignores a new
// --append-system-prompt on --resume, and the model rightly refuses new
// instructions handed over in a message. That is why a new role or new
// instructions restart the agent with a fresh session instead
// (staffing.needsFreshSession). If Claude Code ever honors the flag, the test
// says so: that rule could then be relaxed.
func TestResumeKeepsMemoryAndFollowsNewFlags(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	first := runClaude(t, claudeRun{
		prompt:    "Remember the number 7341. Answer only OK.",
		workDir:   workDir,
		extraArgs: []string{"--append-system-prompt", "Your codename is ALPHA.", "--permission-mode", "manual"},
	})

	args := []string{
		"-p", "What number did I ask you to remember, and what is your codename? Answer as: <number> <codename>.",
		"--resume", first.SessionID,
		"--model", "sonnet", "--output-format", "stream-json", "--verbose", "--setting-sources", "local",
		"--append-system-prompt", "Your codename is BRAVO.",
		"--permission-mode", "acceptEdits",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{"added-on-resume":{"command":"false"}}}`,
	}
	out := runCLI(t, workDir, nil, args)

	init := streamInit(t, out)
	if init.PermissionMode != "acceptEdits" || !strings.Contains(init.Model, "sonnet") {
		t.Errorf("resumed session runs %q in mode %q, want sonnet in acceptEdits", init.Model, init.PermissionMode)
	}

	if len(init.MCPServers) != 1 || init.MCPServers[0].Name != "added-on-resume" {
		t.Errorf("resumed session MCP servers = %v, want only added-on-resume", init.MCPServers)
	}

	answer := streamResult(t, out)
	if !strings.Contains(answer, "7341") {
		t.Errorf("the resumed session lost its memory: %q", answer)
	}

	if strings.Contains(answer, "BRAVO") {
		t.Logf("Claude Code now honors --append-system-prompt on resume (answer %q): "+
			"staffing.needsFreshSession could let instruction changes resume", answer)
	}
}

type streamInitEvent struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"` //nolint:tagliatelle // Claude Code's format
	MCPServers     []struct {
		Name string `json:"name"`
	} `json:"mcp_servers"`
}

// streamInit returns the init event of a stream-json session.
func streamInit(t *testing.T, out []byte) streamInitEvent {
	t.Helper()

	var found streamInitEvent

	eachEvent(out, func(line []byte) bool {
		var ev streamInitEvent
		if json.Unmarshal(line, &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			found = ev

			return false
		}

		return true
	})

	if found.Type == "" {
		t.Fatalf("no init event in claude output:\n%s", out)
	}

	return found
}

// streamResult returns the final answer of a stream-json session.
func streamResult(t *testing.T, out []byte) string {
	t.Helper()

	result, found := "", false

	eachEvent(out, func(line []byte) bool {
		var ev struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Type == "result" {
			result, found = ev.Result, true

			return false
		}

		return true
	})

	if !found {
		t.Fatalf("no result event in claude output:\n%s", out)
	}

	return result
}

// eachEvent calls fn for every line of a stream-json output until it returns
// false.
func eachEvent(out []byte, fn func(line []byte) bool) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		if !fn(scanner.Bytes()) {
			return
		}
	}
}

// An agent working in a worktree (outside the project) cannot write the
// project's .nobody/ or .claude/ either: the absolute deny rules ("//...")
// cover the main checkout, which the daemon reads (runtime.protectedRules).
// The agent is allowed Write (as the dev template is), and a file elsewhere
// in the project is the control.
func TestDenyRulesProtectTheMainCheckout(t *testing.T) {
	t.Parallel()

	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{".nobody", ".claude"} {
		if err := os.MkdirAll(filepath.Join(project, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	worktree := t.TempDir()
	rules := []string{}

	for _, dir := range []string{".nobody", ".claude"} {
		abs := "/" + filepath.Join(project, dir)
		rules = append(rules, `"Edit(`+abs+`/**)"`)
	}

	settings := `{"permissions":{"deny":[` + strings.Join(rules, ",") + `]}}`

	runClaude(t, claudeRun{
		prompt: "Use the Write tool with absolute paths to create three files, each containing the word x: " +
			filepath.Join(project, "allowed.txt") + ", then " + filepath.Join(project, ".nobody", "hacked.txt") +
			", then " + filepath.Join(project, ".claude", "settings.local.json") + ". If one cannot be written, say so and continue.",
		workDir:   worktree,
		extraArgs: []string{"--permission-mode", "acceptEdits", "--allowedTools", "Write", "--add-dir", project, "--settings", settings},
	})

	if _, err := os.Stat(filepath.Join(project, "allowed.txt")); err != nil {
		t.Fatalf("control: the agent should be able to write elsewhere in the project: %v", err)
	}

	for _, protected := range []string{".nobody/hacked.txt", ".claude/settings.local.json"} {
		if _, err := os.Stat(filepath.Join(project, protected)); err == nil {
			t.Errorf("the absolute deny rule did not protect %s", protected)
		}
	}
}
