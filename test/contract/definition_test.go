//go:build contract

package contract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The permission modes used by the profiles (internal/runtime/permissions.go)
// are accepted by Claude Code and reported back in the session's init event.
func TestPermissionModesOfTheProfiles(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"default", "acceptEdits", "auto"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			args := []string{
				"-p", "Answer OK.", "--model", "haiku", "--output-format", "stream-json", "--verbose",
				"--setting-sources", "local", "--permission-mode", mode,
			}

			if got := initPermissionMode(t, runCLI(t, "", nil, args)); got != mode {
				t.Fatalf("init event reports permission mode %q, want %q", got, mode)
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

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var ev struct {
			Type           string `json:"type"`
			Subtype        string `json:"subtype"`
			PermissionMode string `json:"permissionMode"` //nolint:tagliatelle // Claude Code's format
		}
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			return ev.PermissionMode
		}
	}

	t.Fatalf("no init event in claude output:\n%s", out)

	return ""
}
