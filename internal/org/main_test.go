package org

import (
	"os"
	"testing"
)

// TestMain points CLAUDE_CONFIG_DIR to an empty directory, so that loading a
// project never reads the user's own ~/.claude.json and its MCP servers
// (ADR-0010).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nobody-claude-config-*")
	if err != nil {
		panic(err)
	}

	if err := os.Setenv("CLAUDE_CONFIG_DIR", dir); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(dir) // a leftover temporary directory is harmless

	os.Exit(code)
}
