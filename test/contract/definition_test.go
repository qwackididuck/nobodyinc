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

	settings := `{"permissions":{"deny":["Edit(./.nobody/**)","Write(./.nobody/**)"]}}`

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
// environment, so .nobody/mcp.yaml never holds secrets and the values never
// appear on the command line (ADR-0004).
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

// A resumed session keeps its memory and takes the new model, permission mode
// and MCP servers into account. This is how an edited agent restarts
// (runtime.Supervisor.Apply).
//
// It does NOT take a new --append-system-prompt: Claude Code keeps the one
// the session started with. So the new definition travels in the first
// message (runtime.resumeMessage), which must override the old one. If this
// test starts failing on the ALPHA check, Claude Code now honors the flag
// and resumeMessage can be simplified.
func TestResumeAppliesTheNewDefinition(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	first := runClaude(t, claudeRun{
		prompt:    "Remember the number 7341. Answer only OK.",
		workDir:   workDir,
		extraArgs: []string{"--append-system-prompt", "Your codename is ALPHA.", "--permission-mode", "manual"},
	})

	question := "What number did I ask you to remember, and what is your codename? Answer as: <number> <codename>."

	ignored := runClaude(t, claudeRun{
		prompt:    question,
		workDir:   workDir,
		extraArgs: []string{"--resume", first.SessionID, "--fork-session", "--append-system-prompt", "Your codename is BRAVO."},
	})
	if !strings.Contains(ignored.Result, "ALPHA") {
		t.Logf("Claude Code now honors --append-system-prompt on resume (answer %q): runtime.resumeMessage can be simplified", ignored.Result)
	}

	args := []string{
		"-p", "Nobody Inc restarted your session with your current definition. It replaces the system prompt you started with: " +
			"from now on, follow the instructions below.\n\n<current_definition>\nYour codename is BRAVO.\n</current_definition>\n\n" + question,
		"--resume", first.SessionID,
		"--model", "sonnet", "--output-format", "stream-json", "--verbose", "--setting-sources", "local",
		"--append-system-prompt", "Your codename is BRAVO. Any earlier codename is obsolete.",
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

	if !strings.Contains(answer, "BRAVO") || strings.Contains(answer, "ALPHA") {
		t.Errorf("the definition handed over in the first message did not override the old system prompt: %q", answer)
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
